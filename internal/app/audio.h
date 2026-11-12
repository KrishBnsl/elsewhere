#include <stddef.h>

enum { coverSize = 48 };

typedef struct {
    char *title, *artist, *album, *albumArtist;
    int artwork, disc, number;
    char *trackGain, *albumGain;
} Metadata;

typedef struct {
    double elapsed, duration;
    int playing;
} Playback;

Metadata audio_metadata(const char *path);
int audio_artwork(const char *path, int embedded, unsigned char *rgba, unsigned char **png, size_t *pngSize);
int audio_artwork_bytes(const unsigned char *data, size_t size, unsigned char *rgba, unsigned char **png, size_t *pngSize);
char *audio_start(const char *path, float volume);
int audio_start_live(const char *address, float volume);
int audio_live_status(void);
Playback audio_state(void);
int audio_resume(void);
void audio_pause(void);
void audio_seek(double seconds);
void audio_volume(float volume);
void audio_stop(void);
int audio_output_volume(void);
int audio_spectrum(double seconds, float *bands);
char *audio_devices(void);
int audio_select_device(unsigned int id);
void audio_media_start(void);
void audio_media_stop(void);
int audio_media_poll(void);
void audio_media_update(const char *title, const char *artist, const char *album,
                        double elapsed, double duration, int playing);
int audio_prepare_next(const char *path, float volume, double overlap);
void audio_cancel_next(void);
int audio_adopt_next(void);
