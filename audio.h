#include <stddef.h>

enum { coverSize = 48 };

typedef struct {
    char *title, *artist, *album, *albumArtist;
    int artwork;
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
