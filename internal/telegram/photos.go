package telegram

import (
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/megazvezda/laziness/internal/validations"
	"github.com/piglig/go-qr/v2"
)

const (
	photoRetention = 5 * time.Minute
	maxPhotoBytes  = 20 << 20
	photoPrefix    = "telegram-photo-"
)

// isGroupPhoto routes photo messages from groups and supergroups to the photo handler.
// The authorization middleware checks whether the group is allowlisted.
func isGroupPhoto(update *models.Update) bool {
	if update == nil || update.Message == nil || len(update.Message.Photo) == 0 {
		return false
	}
	chat := update.Message.Chat.Type
	return chat == models.ChatTypeGroup || chat == models.ChatTypeSupergroup
}

func resolveQR(img image.Image) (string, error) {
	res, err := qr.Decode(img)
	if err != nil {
		return "", err
	}
	link := strings.TrimSpace(res.Text)
	u, err := url.Parse(link)
	if err != nil || validations.ValidateMoodleLink(link) != nil {
		return "", fmt.Errorf("QR code does not contain a valid Moodle link")
	}
	sessionID, err := strconv.ParseInt(u.Query().Get("sessid"), 10, 64)
	if u.Path != "/mod/attendance/attendance.php" || err != nil || sessionID <= 0 {
		return "", fmt.Errorf("link must point to a Moodle attendance session")
	}
	return link, nil
}

// Telegram provides several sizes of the same photo. Download only the
// largest so small QR codes have the best chance of staying readable
func (b *Bot) handleGroupPhoto(ctx context.Context, client *bot.Bot, update *models.Update) {
	message := update.Message
	photo := message.Photo[0]
	for _, size := range message.Photo[1:] {
		if size.Width*size.Height > photo.Width*photo.Height {
			photo = size
		}
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	file, err := client.GetFile(ctx, &bot.GetFileParams{FileID: photo.FileID})
	if err != nil {
		b.logger.WarnContext(ctx, "Could not get Telegram photo", "error_kind", errorKind(err))
		return
	}
	if file.FilePath == "" {
		b.logger.WarnContext(ctx, "Telegram did not provide a photo download path")
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, client.FileDownloadLink(file), nil)
	if err != nil {
		b.logger.WarnContext(ctx, "Could not build Telegram photo download request")
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		b.logger.WarnContext(ctx, "Could not download Telegram photo", "error_kind", errorKind(err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b.logger.WarnContext(ctx, "Telegram photo download failed", "status", resp.StatusCode)
		return
	}

	photoFile, err := os.CreateTemp(b.photoDir, photoPrefix+"*")
	if err != nil {
		b.logger.WarnContext(ctx, "Could not create Telegram photo file", "error", err)
		return
	}
	defer photoFile.Close()
	b.removePhotoAfter(photoFile.Name(), photoRetention)
	size, err := io.Copy(photoFile, io.LimitReader(resp.Body, maxPhotoBytes+1))
	if err != nil || size > maxPhotoBytes {
		b.logger.WarnContext(ctx, "Could not save Telegram photo", "size", size, "error", err)
		return
	}
	if _, err := photoFile.Seek(0, io.SeekStart); err != nil {
		b.logger.WarnContext(ctx, "Could not read saved Telegram photo", "error", err)
		return
	}
	img, _, err := image.Decode(photoFile)
	if err != nil {
		return
	}
	if _, err := resolveQR(img); err == nil {
		b.logger.InfoContext(ctx, "Moodle attendance QR detected", "chat_id", message.Chat.ID, "message_id", message.ID)
	}
}

func (b *Bot) preparePhotoCache() error {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return err
	}
	b.photoDir = filepath.Join(cacheDir, "laziness", "photos")
	if err := os.MkdirAll(b.photoDir, 0700); err != nil {
		return err
	}
	entries, err := os.ReadDir(b.photoDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasPrefix(entry.Name(), photoPrefix) {
			continue
		}
		info, err := entry.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		b.removePhotoAfter(filepath.Join(b.photoDir, entry.Name()), photoRetention-time.Since(info.ModTime()))
	}
	return nil
}

func (b *Bot) removePhotoAfter(path string, delay time.Duration) {
	time.AfterFunc(delay, func() {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			b.logger.Warn("Could not remove expired Telegram photo", "error", err)
		}
	})
}
