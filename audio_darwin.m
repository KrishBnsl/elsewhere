//go:build darwin && !dhunn_audio

#import <AVFoundation/AVFoundation.h>
#import <Accelerate/Accelerate.h>
#import <CoreAudio/CoreAudio.h>
#import <ImageIO/ImageIO.h>
#import "audio.h"

enum { fftSize = 2048, bandCount = 24, chunkSize = 16384 };
static AVAudioPlayer *player;
static AVPlayer *livePlayer;
static NSURL *playingURL;
static AVAudioFile *analysisFile;
static AVAudioPCMBuffer *chunk;
static AVAudioFramePosition chunkStart, analysisLength;
static BOOL analysisOpened;
static vDSP_DFT_Setup fft;
static int firstBin[bandCount], lastBin[bandCount];
static float window[fftSize], input[fftSize], zeros[fftSize], real[fftSize], imaginary[fftSize];

static NSURL *fileURL(const char *path) {
    return [NSURL fileURLWithFileSystemRepresentation:path isDirectory:NO relativeToURL:nil];
}

Metadata audio_metadata(const char *path) {
    @autoreleasepool {
        Metadata result = {0};
        AVURLAsset *asset = [AVURLAsset URLAssetWithURL:fileURL(path) options:nil];
        for (AVMetadataItem *item in asset.commonMetadata) {
            if ([item.commonKey isEqual:AVMetadataCommonKeyArtwork]) result.artwork = 1;
            char **field = NULL;
            if ([item.commonKey isEqual:AVMetadataCommonKeyTitle]) field = &result.title;
            if ([item.commonKey isEqual:AVMetadataCommonKeyArtist]) field = &result.artist;
            if ([item.commonKey isEqual:AVMetadataCommonKeyAlbumName]) field = &result.album;
            if ([item.identifier isEqual:AVMetadataIdentifieriTunesMetadataAlbumArtist] ||
                [item.identifier isEqual:AVMetadataIdentifierID3MetadataBand]) field = &result.albumArtist;
            if (field && !*field && item.stringValue.UTF8String) {
                *field = strdup(item.stringValue.UTF8String);
            }
        }
        if (!result.albumArtist && result.album) {
            for (AVMetadataItem *item in asset.metadata) {
                if ([item.identifier isEqual:AVMetadataIdentifieriTunesMetadataAlbumArtist] ||
                    [item.identifier isEqual:AVMetadataIdentifierID3MetadataBand]) {
                    const char *artist = item.stringValue.UTF8String;
                    if (artist && *artist) {
                        result.albumArtist = strdup(artist);
                        break;
                    }
                }
            }
        }
        return result;
    }
}

static int renderArtwork(CGImageSourceRef source, unsigned char *rgba, unsigned char **png, size_t *pngSize) {
    if (!source) return 0;
    NSDictionary *options = @{
        (__bridge NSString *)kCGImageSourceCreateThumbnailFromImageAlways: @YES,
        (__bridge NSString *)kCGImageSourceCreateThumbnailWithTransform: @YES,
        (__bridge NSString *)kCGImageSourceThumbnailMaxPixelSize: @512
    };
    CGImageRef image = CGImageSourceCreateThumbnailAtIndex(source, 0, (__bridge CFDictionaryRef)options);
    CFRelease(source);
    if (!image) return 0;
    CGColorSpaceRef space = CGColorSpaceCreateDeviceRGB();
    CGContextRef context = CGBitmapContextCreate(rgba, coverSize, coverSize, 8, coverSize * 4,
                                                space, kCGImageAlphaPremultipliedLast);
    CGColorSpaceRelease(space);
    if (context) {
        CGContextSetInterpolationQuality(context, kCGInterpolationHigh);
        CGContextDrawImage(context, CGRectMake(0, 0, coverSize, coverSize), image);
        CGContextRelease(context);
    }
    if (context) {
        NSMutableData *encoded = [NSMutableData data];
        CGImageDestinationRef destination = CGImageDestinationCreateWithData(
            (__bridge CFMutableDataRef)encoded, CFSTR("public.png"), 1, NULL);
        if (destination) {
            CGImageDestinationAddImage(destination, image, NULL);
            if (CGImageDestinationFinalize(destination) && encoded.length) {
                *png = malloc(encoded.length);
                if (*png) {
                    memcpy(*png, encoded.bytes, encoded.length);
                    *pngSize = encoded.length;
                }
            }
            CFRelease(destination);
        }
    }
    CGImageRelease(image);
    return context != NULL;
}

int audio_artwork(const char *path, int embedded, unsigned char *rgba, unsigned char **png, size_t *pngSize) {
    @autoreleasepool {
        NSURL *url = fileURL(path);
        CGImageSourceRef source = NULL;
        if (embedded) {
            AVURLAsset *asset = [AVURLAsset URLAssetWithURL:url options:nil];
            for (AVMetadataItem *item in asset.commonMetadata) {
                if ([item.commonKey isEqual:AVMetadataCommonKeyArtwork] && item.dataValue) {
                    source = CGImageSourceCreateWithData((__bridge CFDataRef)item.dataValue, NULL);
                    if (source) break;
                }
            }
        } else {
            source = CGImageSourceCreateWithURL((__bridge CFURLRef)url, NULL);
        }
        return renderArtwork(source, rgba, png, pngSize);
    }
}

int audio_artwork_bytes(const unsigned char *data, size_t size, unsigned char *rgba, unsigned char **png, size_t *pngSize) {
    @autoreleasepool {
        NSData *image = [NSData dataWithBytesNoCopy:(void *)data length:size freeWhenDone:NO];
        return renderArtwork(CGImageSourceCreateWithData((__bridge CFDataRef)image, NULL), rgba, png, pngSize);
    }
}

char *audio_start(const char *path, float volume) {
    @autoreleasepool {
        NSURL *url = fileURL(path);
        NSError *error = nil;
        AVAudioPlayer *next = [[AVAudioPlayer alloc] initWithContentsOfURL:url error:&error];
        next.volume = volume;
        if (![next play]) {
            const char *message = error.localizedDescription.UTF8String;
            return strdup(message ?: "Audio device could not start playback");
        }
        [livePlayer pause];
        livePlayer = nil;
        [player stop];
        player = next;
        playingURL = url;
        analysisOpened = NO;
        analysisFile = nil;
        chunk = nil;
        return NULL;
    }
}

int audio_start_live(const char *address, float volume) {
    @autoreleasepool {
        NSURL *url = [NSURL URLWithString:[NSString stringWithUTF8String:address]];
        if (!url || ![url.scheme isEqualToString:@"https"]) return 0;
        AVPlayer *next = [AVPlayer playerWithURL:url];
        if (!next) return 0;
        next.volume = volume;
        [livePlayer pause];
        [player stop];
        player = nil;
        playingURL = nil;
        analysisFile = nil;
        chunk = nil;
        livePlayer = next;
        [livePlayer playImmediatelyAtRate:1.0];
        return 1;
    }
}

int audio_live_status(void) {
    @autoreleasepool {
        CFRunLoopRunInMode(kCFRunLoopDefaultMode, 0.001, true);
        if (!livePlayer || livePlayer.status == AVPlayerStatusFailed ||
            livePlayer.currentItem.status == AVPlayerItemStatusFailed) return -1;
        return livePlayer.timeControlStatus == AVPlayerTimeControlStatusPlaying;
    }
}

Playback audio_state(void) {
    @autoreleasepool {
        if (livePlayer) {
            return (Playback){0, 0, livePlayer.timeControlStatus == AVPlayerTimeControlStatusPlaying};
        }
        return (Playback){player.currentTime, player.duration, player.isPlaying};
    }
}
int audio_resume(void) { return [player play]; }
void audio_pause(void) { [livePlayer pause]; [player pause]; }
void audio_seek(double seconds) { if (!livePlayer) player.currentTime = seconds; }
void audio_volume(float volume) { livePlayer.volume = volume; player.volume = volume; }
void audio_stop(void) { [livePlayer pause]; livePlayer = nil; [player stop]; player = nil; }

static BOOL property(AudioObjectID device, UInt32 selector, UInt32 scope, UInt32 element, void *value, UInt32 size) {
    AudioObjectPropertyAddress address = {selector, scope, element};
    return AudioObjectGetPropertyData(device, &address, 0, NULL, &size, value) == noErr;
}

int audio_output_volume(void) {
    AudioDeviceID device = kAudioObjectUnknown;
    if (!property(kAudioObjectSystemObject, kAudioHardwarePropertyDefaultOutputDevice,
                  kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyElementMain, &device, sizeof(device))
        || device == kAudioObjectUnknown) return -1;

    UInt32 scope = kAudioDevicePropertyScopeOutput;
    UInt32 mute = 0;
    if (property(device, kAudioDevicePropertyMute, scope, kAudioObjectPropertyElementMain, &mute, sizeof(mute)) && mute) return 0;
    Float32 level = 0;
    if (property(device, kAudioDevicePropertyVolumeScalar, scope, kAudioObjectPropertyElementMain, &level, sizeof(level))) {
        return lroundf(level * 100);
    }
    float total = 0;
    int count = 0;
    for (int channel = 1; channel <= 2; channel++) {
        if (property(device, kAudioDevicePropertyVolumeScalar, scope, channel, &level, sizeof(level))) {
            total += level;
            count++;
        }
    }
    return count ? lroundf(total * 100 / count) : -1;
}

static void openAnalysis(void) {
    analysisOpened = YES;
    AVAudioFile *file = [[AVAudioFile alloc] initForReading:playingURL commonFormat:AVAudioPCMFormatFloat32
                                              interleaved:NO error:NULL];
    double rate = file.processingFormat.sampleRate;
    if (rate <= 90) return;
    if (!fft) {
        fft = vDSP_DFT_zop_CreateSetup(NULL, fftSize, vDSP_DFT_FORWARD);
        if (!fft) return;
        for (int i = 0; i < fftSize; i++) window[i] = 0.5 - 0.5 * cos(2 * M_PI * i / (fftSize - 1));
    }
    chunk = [[AVAudioPCMBuffer alloc] initWithPCMFormat:file.processingFormat frameCapacity:chunkSize];
    if (!chunk) return;
    analysisFile = file;
    analysisLength = file.length;
    chunkStart = 0;
    double ratio = fmin(rate / 2, 16000) / 45;
    for (int band = 0; band < bandCount; band++) {
        firstBin[band] = fmin(fftSize / 2 - 1, fmax(1, ceil(45 * pow(ratio, (double)band / bandCount) * fftSize / rate)));
        lastBin[band] = fmin(fftSize / 2, fmax(firstBin[band] + 1, ceil(45 * pow(ratio, (double)(band + 1) / bandCount) * fftSize / rate)));
    }
}

int audio_spectrum(double seconds, float *bands) {
    @autoreleasepool {
        if (!analysisOpened) openAnalysis();
        if (!analysisFile) return 0;
        AVAudioFramePosition offset = fmax(0, seconds) * analysisFile.processingFormat.sampleRate;
        if (offset >= analysisLength) return 1;
        AVAudioFramePosition end = MIN(analysisLength, offset + fftSize);
        if (offset < chunkStart || end > chunkStart + chunk.frameLength) {
            // Keep decoding forward between nearby frames instead of seeking a compressed file on every redraw.
            if (offset < analysisFile.framePosition || offset - analysisFile.framePosition >= chunkSize / 2) {
                analysisFile.framePosition = offset;
            }
            chunkStart = analysisFile.framePosition;
            if (![analysisFile readIntoBuffer:chunk error:NULL]) return 0;
            if (end > chunkStart + chunk.frameLength) {
                analysisFile.framePosition = offset;
                chunkStart = offset;
                if (![analysisFile readIntoBuffer:chunk error:NULL]) return 0;
            }
        }
        float * const *channels = chunk.floatChannelData;
        if (!channels) return 0;
        int begin = (int)(offset - chunkStart);
        int count = MIN(fftSize, (int)chunk.frameLength - begin);
        if (begin < 0 || count <= 0) return 1;
        for (int channel = 0; channel < MIN(2, chunk.format.channelCount); channel++) {
            vDSP_vmul(channels[channel] + begin, 1, window, 1, input, 1, count);
            memset(input + count, 0, (fftSize - count) * sizeof(float));
            vDSP_DFT_Execute(fft, input, zeros, real, imaginary);
            for (int band = 0; band < bandCount; band++) {
                for (int bin = firstBin[band]; bin < lastBin[band]; bin++) {
                    bands[band] = fmaxf(bands[band], real[bin] * real[bin] + imaginary[bin] * imaginary[bin]);
                }
            }
        }
        for (int band = 0; band < bandCount; band++) {
            float amplitude = sqrtf(bands[band]) * 4 / fftSize;
            bands[band] = fminf(1, fmaxf(0, (20 * log10f(fmaxf(amplitude, 0.000001f)) + 54) / 49));
        }
        return 1;
    }
}
