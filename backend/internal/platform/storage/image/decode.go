package image

import (
	"bytes"
	"image"

	"golang.org/x/image/webp"
)

// webpConfig lee la cabecera sin decodificar todo el frame.
func webpConfig(data []byte) (image.Config, error) {
	cfg, err := webp.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return image.Config{}, err
	}
	return cfg, nil
}

// webpDecode decodifica la imagen completa.
func webpDecode(data []byte) (image.Image, error) {
	return webp.Decode(bytes.NewReader(data))
}
