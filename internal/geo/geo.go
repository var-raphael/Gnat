package geo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client resolves an IP to a country code using two free, keyless
// services: ip2c.org as primary, country.is as fallback if the primary
// fails or times out. Neither requires an account or API token.
type Client struct {
	httpClient *http.Client
}

func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: 4 * time.Second,
		},
	}
}

// Lookup returns the ISO country code for the given IP, or "" if the IP
// is unresolvable (e.g. localhost during dev) or both services fail.
// Errors are deliberately non-fatal: this runs in the background and a
// missing country should never affect event ingestion.
func (c *Client) Lookup(ip string) string {
	if ip == "" {
		return ""
	}

	if code := c.lookupIP2C(ip); code != "" {
		return code
	}

	return c.lookupCountryIs(ip)
}

// lookupIP2C calls ip2c.org, which returns plain text like
// "1;US;USA;United States of America (the)" on success.
func (c *Client) lookupIP2C(ip string) string {
	url := fmt.Sprintf("https://ip2c.org/%s", ip)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	buf := make([]byte, 256)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])

	parts := strings.Split(body, ";")
	// success format: "1;US;USA;United States of America (the)"
	if len(parts) < 2 || parts[0] != "1" {
		return ""
	}
	return parts[1]
}

// countryIsResponse matches the JSON shape from api.country.is/{ip}.
type countryIsResponse struct {
	Country string `json:"country"`
	IP      string `json:"ip"`
}

// lookupCountryIs calls country.is as a fallback if ip2c.org is
// unavailable or returns no result.
func (c *Client) lookupCountryIs(ip string) string {
	url := fmt.Sprintf("https://api.country.is/%s", ip)

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ""
	}

	var parsed countryIsResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return ""
	}
	return parsed.Country
}
