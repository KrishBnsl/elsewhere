package app

func (a *album) updateRadioArtwork() {
	if a.radioArtwork == nil {
		return
	}
	select {
	case art := <-a.radioArtwork:
		a.radioArtwork = nil
		if art != nil {
			a.cover = art
			a.artworkSize = 0
		}
	default:
	}
}
