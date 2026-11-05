package main

import (
	"image"
	"log/slog"
	"os"

	"github.com/piglig/go-qr/v2"
	"github.com/pkg/browser"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	slog.SetDefault(logger)
}

func openLink(link string) error {
	slog.Info("Opening link", "url", link)
	err := browser.OpenURL(link)
	if err != nil {
		slog.Error("Couldn't open link", "error", err, "url", link)
	}
	return nil
}

func resolveQR(img image.Image) *qr.DecodeResult {
	slog.Info("Decoding the image", "width", img.Bounds().Dx(), "height", img.Bounds().Dy())
	res, err := qr.Decode(img)
	if err != nil {
		slog.Error("Couldn't decode the image", "error", err, "width", img.Bounds().Dx(), "height", img.Bounds().Dy())
	}
	return res
}
