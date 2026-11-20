package internal

import (
	"fmt"
	"image"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"sync"

	"github.com/megazvezda/laziness/internal/validations"
	"github.com/piglig/go-qr/v2"
	"github.com/pkg/browser"
)

type AttendanceService struct {
	mu       sync.Mutex
	sessions map[int64]*studentSession
}

type studentSession struct {
	mu     sync.Mutex
	client *http.Client
}

func (s *AttendanceService) getOrCreateSession(moodleUserID int64) (*studentSession, error) {
	// lock the service mutex
	s.mu.Lock()
	defer s.mu.Unlock()
	// if map has a session for id, return it
	if session, ok := s.sessions[moodleUserID]; ok {
		return session, nil
	}
	// otherwise create a jar and a client, store session in map and return it,
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("jar creation: %w", err)
	}
	newClient := &http.Client{
		Jar: jar,
	}
	session := &studentSession{
		client: newClient,
	}
	if s.sessions == nil {
		s.sessions = make(map[int64]*studentSession)
	}
	s.sessions[moodleUserID] = session
	return session, nil
	// use defer to unlock on every return path.

}

func MarkAttendance(link string) error {
	// need to call auto-login key for each token, get a key, follow to "https://moodle.vilniustech.lt/admin/tool/mobile/autologin.php?userid=4721&key=[key]&urltogo=attendancelink"
	// problem is
	return nil
}

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
