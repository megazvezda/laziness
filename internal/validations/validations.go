package validations

import (
	"fmt"
	"net/url"
)

/*
keep in mind: if codes will be held on a different domain to be resolved later into appropriate link,
we'll have to add the function to resolve the redirects
*/

// ValidateMoodleLink checks if a string uses "https" scheme and "moodle.vilniustech.lt" host
func ValidateMoodleLink(link string) error {
	u, err := url.Parse(link)
	if err != nil {
		return fmt.Errorf("invalid link: %w", err)
	}
	if u.Scheme != "https" || u.Host != "moodle.vilniustech.lt" || u.User != nil {
		return fmt.Errorf("link must use https://moodle.vilniustech.lt")
	}
	return nil
}
