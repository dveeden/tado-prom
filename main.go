package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

type TadoRateLimit struct {
	LimitType string
	Remaining int
	Refill    int
}

func rateLimiteFromHeader(h string) TadoRateLimit {
	parts := strings.Split(h, ";")
	t := TadoRateLimit{}
	t.LimitType = strings.Trim(parts[0], "\"") // "ratelimit"
	for _, p := range parts[1:] {
		pparts := strings.SplitN(p, "=", 2)
		k := pparts[0]
		v := pparts[1]
		switch k {
		case "r":
			t.Remaining, _ = strconv.Atoi(v)
		case "t":
			t.Refill, _ = strconv.Atoi(v)
		}
	}
	return t
}

type TadoDevAuth struct {
	DeviceCode              string `json:"device_code"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
	UserCode                string `json:"user_code"`
	VerificationUrl         string `json:"verification_uri"`
	VerificationUrlComplete string `json:"verification_uri_complete"`
}

type TadoToken struct {
	AccessToken  string    `json:"access_token"`
	ExpiresIn    int       `json:"expires_in"`
	RefreshToken string    `json:"refresh_token"`
	Scope        string    `json:"scope"`
	Tokentype    string    `json:"token_type"`
	UserId       string    `json:"userId"`
	Expiry       time.Time `json:"expiry"`
}

type TadoHome struct {
	Id   uint   `json:"id"`
	Name string `json:"name"`
}

type TadoMe struct {
	Name     string     `json:"name"`
	Email    string     `json:"email"`
	Username string     `json:"username"`
	Id       string     `json:"id"` // UUID
	Homes    []TadoHome `json:"homes"`
	Locale   string     `json:"locale"`
	// mobileDevices: ignored
}

type TadoTemperature struct {
	Value float64 `json:"value"`
}

type TadoPercentage struct {
	Percentage int `json:percentage`
}

type TadoSensorDataPoints struct {
	InsideTemperature TadoTemperature `json:"insideTemperature"`
	Humidity          TadoPercentage  `json:"humidity"`
}

type TadoSetting struct {
	Power       string          `json:"power"`
	Temperature TadoTemperature `json:"temperature"`
}

type TadoRoom struct {
	Id               uint                 `json:"id"`
	Name             string               `json:"name"`
	SensorDataPoints TadoSensorDataPoints `json:"sensorDataPoints"`
	Setting          TadoSetting          `json:"setting"`
	HeatingPower     TadoPercentage       `json:"heatingPower"`
}

type Tado struct {
	client  *http.Client
	homeids []uint
	token   TadoToken
}

func NewTado() *Tado {
	return &Tado{
		client: &http.Client{},
	}
}

func (t *Tado) Authenticate() error {
	resp, err := t.client.PostForm("https://login.tado.com/oauth2/device_authorize",
		url.Values{
			"client_id": {"1bb50063-6b0c-4d11-bd99-387f4a91cc46"},
			"scope":     {"offline_access"},
		})
	if err != nil {
		return err
	}
	var devauth TadoDevAuth
	err = json.NewDecoder(resp.Body).Decode(&devauth)
	if err != nil {
		return err
	}
	resp.Body.Close()
	fmt.Println(devauth.VerificationUrlComplete)
	fmt.Println("press enter to continue")
	fmt.Scanln()

	resp, err = t.client.PostForm("https://login.tado.com/oauth2/token",
		url.Values{
			"client_id":   {"1bb50063-6b0c-4d11-bd99-387f4a91cc46"},
			"device_code": {devauth.DeviceCode},
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		})
	if err != nil {
		return err
	}
	err = json.NewDecoder(resp.Body).Decode(&t.token)
	if err != nil {
		return err
	}
	resp.Body.Close()
	t.token.Expiry = time.Now().Add(time.Second * time.Duration(t.token.ExpiresIn-30))

	return nil
}

func (t *Tado) RefreshToken() error {
	resp, err := t.client.PostForm("https://login.tado.com/oauth2/token",
		url.Values{
			"client_id":     {"1bb50063-6b0c-4d11-bd99-387f4a91cc46"},
			"grant_type":    {"refresh_token"},
			"refresh_token": {t.token.RefreshToken},
		})
	if err != nil {
		return err
	}
	var token TadoToken
	err = json.NewDecoder(resp.Body).Decode(&token)
	if err != nil {
		return err
	}
	resp.Body.Close()
	t.token = token

	t.token.Expiry = time.Now().Add(time.Second * time.Duration(t.token.ExpiresIn-30))

	return nil
}

func (t *Tado) Metrics(w http.ResponseWriter, req *http.Request) {
	if time.Now().After(t.token.Expiry) {
		slog.Info("refreshing token")
		t.RefreshToken()
	}

	if len(t.homeids) == 0 {
		req, err := http.NewRequest(http.MethodGet, "https://my.tado.com/api/v2/me", nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.Header.Set("Authorization", "Bearer "+t.token.AccessToken)
		resp, err := t.client.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		var me TadoMe
		err = json.NewDecoder(resp.Body).Decode(&me)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, home := range me.Homes {
			if slices.Contains(t.homeids, home.Id) {
				return
			}
			t.homeids = append(t.homeids, home.Id)
		}
		resp.Body.Close()
	}

	for _, homeid := range t.homeids {
		var rooms []TadoRoom
		req, err := http.NewRequest(
			http.MethodGet,
			fmt.Sprintf("https://hops.tado.com/homes/%d/rooms?ngsw-bypass=true", homeid),
			nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.Header.Set("Authorization", "Bearer "+t.token.AccessToken)
		resp, err := t.client.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		io.WriteString(w, fmt.Sprintf("tado_service_status_code %d\n", resp.StatusCode))

		rateLimitInfo := rateLimiteFromHeader(resp.Header.Get("ratelimit"))
		io.WriteString(w, fmt.Sprintf("tado_service_ratelimit{type=\"perday\",kind=\"remain\"} %d\n", rateLimitInfo.Remaining))
		io.WriteString(w, fmt.Sprintf("tado_service_ratelimit{type=\"perday\",kind=\"refill\"} %d\n", rateLimitInfo.Refill))
		if resp.StatusCode < 200 || resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			slog.Warn("failed to get room info",
				slog.Int("StatusCode", resp.StatusCode),
				slog.String("body", string(body)))
			slog.Error("unexpected status code from service", "status_code", resp.StatusCode)
			return
		}
		err = json.NewDecoder(resp.Body).Decode(&rooms)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, room := range rooms {
			io.WriteString(w, fmt.Sprintf("tado_temperature{room=\"%s\",home=\"%d\"} %f\n",
				room.Name, homeid, room.SensorDataPoints.InsideTemperature.Value))
			io.WriteString(w, fmt.Sprintf("tado_humidity{room=\"%s\",home=\"%d\"} %d\n",
				room.Name, homeid, room.SensorDataPoints.Humidity.Percentage))
			io.WriteString(w, fmt.Sprintf("tado_set_temperature{room=\"%s\",home=\"%d\"} %f\n",
				room.Name, homeid, room.Setting.Temperature.Value))
			io.WriteString(w, fmt.Sprintf("tado_heating_power{room=\"%s\",home=\"%d\"} %d\n",
				room.Name, homeid, room.HeatingPower.Percentage))
		}
	}
}

func main() {
	t := NewTado()
	err := t.Authenticate()
	if err != nil {
		panic(err)
	}
	fmt.Println("Listening on http://127.0.0.1:8005/metrics")
	http.HandleFunc("/metrics", t.Metrics)
	log.Fatal(http.ListenAndServe(":8005", nil))
}
