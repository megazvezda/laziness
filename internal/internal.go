package internal

import (
	"context"
	"fmt"
	"image"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"sync"

	"github.com/megazvezda/laziness/internal/validations"
	"github.com/megazvezda/laziness/models"
	"github.com/piglig/go-qr/v2"
	"github.com/pkg/browser"
	"golang.org/x/net/html"
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
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if validations.ValidateMoodleLink(req.URL.String()) != nil {
				return http.ErrUseLastResponse
			}
			if len(via) >= 10 {
				return fmt.Errorf("too many Moodle redirects")
			}
			return nil
		},
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

func (s *AttendanceService) MarkAttendanceForStudent(ctx context.Context, student models.DBRecord, link string) error {
	err := validations.ValidateMoodleLink(link)
	if err != nil {
		return fmt.Errorf("attendance URL must use https://moodle.vilniustech.lt")
	}
	session, err := s.getOrCreateSession(student.MoodleUserID)
	if err != nil {
		return err
	}
	session.mu.Lock()
	defer session.mu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return fmt.Errorf("failed to build attendance request")
	}
	resp, err := session.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("failed to send attendance request")
	}
	defer resp.Body.Close()
	slog.DebugContext(ctx, "Moodle attendance page received",
		"moodle_user_id", student.MoodleUserID,
		"status", resp.StatusCode,
		"path", resp.Request.URL.Path,
	)

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		location, err := resp.Location()
		if err == nil && location.Scheme == "https" &&
			location.Host == "fs.vilniustech.lt" && location.Path == "/adfs/ls/" {
			return fmt.Errorf("Moodle requires university sign-in")
		}
		return fmt.Errorf("unexpected Moodle redirect (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Moodle returned HTTP %d", resp.StatusCode)
	}

	doc, err := html.Parse(resp.Body)
	if err != nil {
		return fmt.Errorf("parse attendance page: %w", err)
	}
	form := findAttendanceForm(doc)
	if form == nil {
		return fmt.Errorf("Moodle response did not contain an attendance form")
	}
	action, err := resp.Request.URL.Parse(attr(form, "action"))
	if err != nil || validations.ValidateMoodleLink(action.String()) != nil {
		return fmt.Errorf("invalid Moodle attendance form action")
	}
	var fields []string
	for n := range form.Descendants() {
		if n.Type == html.ElementNode && n.Data == "input" {
			if name := attr(n, "name"); name != "" {
				fields = append(fields, name)
			}
		}
	}
	slog.InfoContext(ctx, "Attendance form found",
		"method", attr(form, "method"),
		"path", action.Path,
		"fields", fields,
	)
	return nil
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func findAttendanceForm(doc *html.Node) *html.Node {
	const marker = "_qf__mod_attendance_form_studentattendance"
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode || n.Data != "input" ||
			attr(n, "type") != "hidden" || attr(n, "name") != marker {
			continue
		}
		for parent := n.Parent; parent != nil; parent = parent.Parent {
			if parent.Type == html.ElementNode && parent.Data == "form" {
				return parent
			}
		}
	}
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
