#include <stddef.h>

typedef struct Music_Emu RadioDecoder;
int radio_decoder_available(void);
RadioDecoder *radio_decoder_open(const void *data, size_t size, int rate, unsigned int choice, int *milliseconds);
int radio_decoder_play(RadioDecoder *decoder, int count, short *out);
int radio_decoder_ended(RadioDecoder *decoder);
void radio_decoder_close(RadioDecoder *decoder);
