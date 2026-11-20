package internal

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/megazvezda/laziness/internal/auth"
	"github.com/megazvezda/laziness/internal/validations"
	"github.com/megazvezda/laziness/models"
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
			if via[0].Method == http.MethodPost && req.Method != http.MethodGet {
				return http.ErrUseLastResponse
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
	if requiresUniversitySignIn(resp) {
		resp.Body.Close()
		key, err := auth.GetAutoLoginKey(ctx, models.Tokens{
			PublicToken:  student.PublicToken,
			PrivateToken: student.PrivateToken,
		})
		if err != nil {
			return err
		}
		resp, err = session.openAutoLogin(ctx, student.MoodleUserID, key, link)
		if err != nil {
			return err
		}
	}
	defer resp.Body.Close()
	slog.DebugContext(ctx, "Moodle attendance page received",
		"moodle_user_id", student.MoodleUserID,
		"status", resp.StatusCode,
		"path", resp.Request.URL.Path,
	)

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		if requiresUniversitySignIn(resp) {
			return fmt.Errorf("Moodle still requires university sign-in after auto-login")
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
	if err != nil || validations.ValidateMoodleLink(action.String()) != nil ||
		action.Path != "/mod/attendance/attendance.php" ||
		!strings.EqualFold(attr(form, "method"), http.MethodPost) {
		return fmt.Errorf("invalid Moodle attendance form action")
	}
	return session.submitAttendance(ctx, action, form)
}

func (s *studentSession) submitAttendance(ctx context.Context, action *url.URL, form *html.Node) error {
	values, err := attendanceFormValues(form)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, action.String(), strings.NewReader(values.Encode()))
	if err != nil {
		return fmt.Errorf("build Moodle attendance submission")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("Moodle attendance submission could not be confirmed: %w", ctx.Err())
		}
		return fmt.Errorf("Moodle attendance submission could not be confirmed after a request error")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Request.Method != http.MethodGet ||
		resp.Request.URL.Path != "/mod/attendance/view.php" {
		return fmt.Errorf("Moodle attendance submission could not be confirmed (HTTP %d)", resp.StatusCode)
	}
	doc, err := html.Parse(resp.Body)
	if err != nil {
		return fmt.Errorf("read Moodle attendance confirmation: %w", err)
	}
	for n := range doc.Descendants() {
		if n.Type == html.ElementNode && attr(n, "role") == "alert" &&
			strings.Contains(nodeText(n), "Your attendance in this session has been recorded.") {
			return nil
		}
	}
	return fmt.Errorf("Moodle did not show the attendance confirmation")
}

func attendanceFormValues(form *html.Node) (url.Values, error) {
	values := make(url.Values)
	for n := range form.Descendants() {
		if n.Type != html.ElementNode || n.Data != "input" {
			continue
		}
		if slices.ContainsFunc(n.Attr, func(a html.Attribute) bool { return a.Key == "disabled" }) {
			continue
		}
		name, value := attr(n, "name"), attr(n, "value")
		if name == "" {
			continue
		}
		switch attr(n, "type") {
		case "hidden":
			values.Add(name, value)
		case "radio":
			if name != "status" || value == "" {
				continue
			}
			for parent := n.Parent; parent != nil && parent != form; parent = parent.Parent {
				if parent.Type == html.ElementNode && parent.Data == "label" {
					if strings.EqualFold(nodeText(parent), "Present") {
						values.Add(name, value)
					}
					break
				}
			}
		case "submit":
			if name == "submitbutton" {
				values.Add(name, value)
			}
		}
	}
	if len(values["status"]) != 1 {
		return nil, fmt.Errorf("attendance form must offer exactly one Present option")
	}
	if values.Get("sessid") == "" || values.Get("sesskey") == "" || len(values["submitbutton"]) != 1 {
		return nil, fmt.Errorf("Moodle attendance form is missing required submission fields")
	}
	return values, nil
}

func requiresUniversitySignIn(resp *http.Response) bool {
	if resp.StatusCode < 300 || resp.StatusCode >= 400 {
		return false
	}
	location, err := resp.Location()
	return err == nil && location.Scheme == "https" &&
		location.Host == "fs.vilniustech.lt" && location.Path == "/adfs/ls/"
}

func (s *studentSession) openAutoLogin(ctx context.Context, moodleUserID int64, key, attendanceLink string) (*http.Response, error) {
	query := url.Values{
		"userid":  {strconv.FormatInt(moodleUserID, 10)},
		"key":     {key},
		"urltogo": {attendanceLink},
	}
	endpoint := url.URL{
		Scheme:   "https",
		Host:     "moodle.vilniustech.lt",
		Path:     "/admin/tool/mobile/autologin.php",
		RawQuery: query.Encode(),
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build Moodle auto-login request")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("send Moodle auto-login request")
	}
	return resp, nil
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func nodeText(n *html.Node) string {
	var text strings.Builder
	for child := range n.Descendants() {
		if child.Type == html.TextNode {
			text.WriteString(child.Data)
		}
	}
	return strings.Join(strings.Fields(text.String()), " ")
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
