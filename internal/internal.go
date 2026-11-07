package internal

import (
	"fmt"
	"image"
	"log/slog"

	"github.com/megazvezda/laziness/internal/validations"
	"github.com/piglig/go-qr/v2"
	"github.com/pkg/browser"
)

func openLink(link string) error {
	err := validations.ValidateMoodleLink(link)
	if err != nil {
		return fmt.Errorf("opening link: %w", err)
	}
	slog.Info("Opening link", "url", link)

	err = browser.OpenURL(link)
	if err != nil {
		slog.Error("Couldn't open link", "error", err, "url", link)
	}
	return nil // todo
}

func resolveQR(img image.Image) *qr.DecodeResult {
	slog.Info("Decoding the image", "width", img.Bounds().Dx(), "height", img.Bounds().Dy())
	res, err := qr.Decode(img)
	if err != nil {
		slog.Error("Couldn't decode the image", "error", err, "width", img.Bounds().Dx(), "height", img.Bounds().Dy())
	}
	return res
}
