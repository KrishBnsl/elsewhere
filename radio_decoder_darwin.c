#include "radio_decoder.h"
#include <dlfcn.h>
#include <pthread.h>
#include <stdlib.h>

typedef struct {
    int length, intro_length, loop_length, play_length;
    int reserved[12];
} TrackTiming;

static pthread_once_t once = PTHREAD_ONCE_INIT;
static int ready;
static const char *(*open_data)(const void *, long, RadioDecoder **, int);
static int (*track_count)(const RadioDecoder *);
static const char *(*start_track)(RadioDecoder *, int);
static const char *(*play)(RadioDecoder *, int, short *);
static int (*track_ended)(const RadioDecoder *);
static void (*destroy)(RadioDecoder *);
static const char *(*track_info)(const RadioDecoder *, TrackTiming **, int);
static void (*free_info)(TrackTiming *);

static void load_decoder(void) {
    const char *paths[] = {
        getenv("ELSEWHERE_GME_LIBRARY"),
        "/opt/homebrew/opt/game-music-emu/lib/libgme.dylib",
        "/usr/local/opt/game-music-emu/lib/libgme.dylib",
        "libgme.dylib"
    };
    void *library = NULL;
    for (size_t i = 0; i < sizeof(paths) / sizeof(paths[0]) && !library; i++) {
        if (paths[i] && *paths[i]) library = dlopen(paths[i], RTLD_NOW | RTLD_LOCAL);
    }
    if (!library) return;
    open_data = dlsym(library, "gme_open_data");
    track_count = dlsym(library, "gme_track_count");
    start_track = dlsym(library, "gme_start_track");
    play = dlsym(library, "gme_play");
    track_ended = dlsym(library, "gme_track_ended");
    destroy = dlsym(library, "gme_delete");
    track_info = dlsym(library, "gme_track_info");
    free_info = dlsym(library, "gme_free_info");
    ready = open_data && track_count && start_track && play && track_ended && destroy && track_info && free_info;
    if (!ready) dlclose(library);
}

int radio_decoder_available(void) {
    pthread_once(&once, load_decoder);
    return ready;
}

RadioDecoder *radio_decoder_open(const void *data, size_t size, int rate, unsigned int choice, int *milliseconds) {
    RadioDecoder *decoder = NULL;
    if (!radio_decoder_available() || open_data(data, (long)size, &decoder, rate)) return NULL;
    int count = track_count(decoder);
    if (count <= 0) {
        destroy(decoder);
        return NULL;
    }
    int index = choice % (unsigned int)count;
    TrackTiming *timing = NULL;
    if (!track_info(decoder, &timing, index) && timing) {
        *milliseconds = timing->play_length;
        free_info(timing);
    }
    if (start_track(decoder, index)) {
        destroy(decoder);
        return NULL;
    }
    return decoder;
}

int radio_decoder_play(RadioDecoder *decoder, int count, short *out) {
    return play(decoder, count, out) == NULL;
}

int radio_decoder_ended(RadioDecoder *decoder) {
    return track_ended(decoder);
}

void radio_decoder_close(RadioDecoder *decoder) {
    destroy(decoder);
}
