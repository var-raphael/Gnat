package geo

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)


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

func (c *Client) Lookup(ip string) string {
	if ip == "" {
		return ""
	}

	if code := c.lookupIP2C(ip); code != "" {
		return code
	}

	return c.lookupCountryIs(ip)
}


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

	if len(parts) < 2 || parts[0] != "1" {
		return ""
	}
	return parts[1]
}


type countryIsResponse struct {
	Country string `json:"country"`
	IP      string `json:"ip"`
}


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
