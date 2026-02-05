package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"golang.org/x/oauth2"
)

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
	config  *oauth2.Config
	client  *http.Client
	homeids []uint
}

func (t *Tado) Authenticate() error {
	ctx := context.Background()

	response, err := t.config.DeviceAuth(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Now go to %s\n", response.VerificationURIComplete)

	fmt.Println("press enter to continue")
	fmt.Scanln()

	token, err := t.config.DeviceAccessToken(ctx, response)
	if err != nil {
		return err
	}

	t.client = t.config.Client(ctx, token)
	return nil
}

func (t *Tado) Metrics(w http.ResponseWriter, req *http.Request) {
	if len(t.homeids) == 0 {
		resp, err := t.client.Get("https://my.tado.com/api/v2/me")
		if err != nil {
			panic(err)
		}
		var me TadoMe
		err = json.NewDecoder(resp.Body).Decode(&me)
		if err != nil {
			panic(err)
		}
		for _, home := range me.Homes {
			t.homeids = append(t.homeids, home.Id)
		}
		resp.Body.Close()
	}

	for _, homeid := range t.homeids {
		var rooms []TadoRoom
		resp, err := t.client.Get(fmt.Sprintf("https://hops.tado.com/homes/%d/rooms?ngsw-bypass=true", homeid))
		if err != nil {
			panic(err)
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 400 {
			panic(fmt.Errorf("unexpected status code %d", resp.StatusCode))
		}
		err = json.NewDecoder(resp.Body).Decode(&rooms)
		if err != nil {
			panic(err)
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
	t := Tado{
		config: &oauth2.Config{
			ClientID: "1bb50063-6b0c-4d11-bd99-387f4a91cc46",
			Scopes:   []string{"offline_access"},
			Endpoint: oauth2.Endpoint{
				DeviceAuthURL: "https://login.tado.com/oauth2/device_authorize",
				TokenURL:      "https://login.tado.com/oauth2/token",
			},
		},
	}

	t.Authenticate()
	fmt.Println("Listening on http://127.0.0.1:8005/metrics")
	http.HandleFunc("/metrics", t.Metrics)
	log.Fatal(http.ListenAndServe(":8005", nil))
}
