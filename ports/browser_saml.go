package ports

import (
	"errors"
	"html"
	"net/url"
	"regexp"
	"strings"
)

// BrowserSAMLForm is one HTML auto-submit form of the SAML HTTP-POST binding:
// the AuthnRequest Forward hands the browser for the IdP, or the Response the
// IdP hands back for Forward's assertion consumer service.
type BrowserSAMLForm struct {
	Action string
	Fields url.Values
}

var (
	browserFormActionRE = regexp.MustCompile(`(?is)<form[^>]*\baction\s*=\s*["']([^"']+)["']`)
	browserInputTagRE   = regexp.MustCompile(`(?is)<input\b[^>]*>`)
	browserAttrNameRE   = regexp.MustCompile(`(?is)\bname\s*=\s*["']([^"']*)["']`)
	browserAttrValueRE  = regexp.MustCompile(`(?is)\bvalue\s*=\s*["']([^"']*)["']`)
)

// ParseBrowserSAMLForm extracts the action and the named inputs of the first
// form in an HTML page. It is exported so an IdP client can hand Forward the
// same shape it parsed from the IdP.
func ParseBrowserSAMLForm(page string) (*BrowserSAMLForm, error) {
	action := browserFormActionRE.FindStringSubmatch(page)
	if len(action) < 2 {
		return nil, errors.New("forward: page has no form")
	}
	fields := url.Values{}
	for _, tag := range browserInputTagRE.FindAllString(page, -1) {
		name := browserAttrNameRE.FindStringSubmatch(tag)
		if len(name) < 2 || strings.TrimSpace(name[1]) == "" {
			continue
		}
		value := ""
		if v := browserAttrValueRE.FindStringSubmatch(tag); len(v) > 1 {
			value = html.UnescapeString(v[1])
		}
		fields.Set(html.UnescapeString(name[1]), value)
	}
	return &BrowserSAMLForm{Action: html.UnescapeString(action[1]), Fields: fields}, nil
}
