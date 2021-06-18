package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/go-redis/redis/v8"
)

func main() {
	fmt.Println("starting server")

	api := NewAPI()

	http.HandleFunc("/api", api.Handler)

	http.ListenAndServe(fmt.Sprintf(":%s", os.Getenv("PORT")), nil)
}

func (a *API) Handler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("in the handler")

	q := r.URL.Query().Get("q")
	data, cacheHit, err := a.getData(r.Context(), q)
	if err != nil {
		fmt.Printf("error calling data source: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	resp := APIResponse{
		Cache: cacheHit,
		Data:  data,
	}

	err = json.NewEncoder(w).Encode(resp)
	if err != nil {
		fmt.Printf("error encoding response: %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (a *API) getData(ctx context.Context, q string) ([]NominatimResponse, bool, error) {
	// is query cached?
	value, err := a.cache.Get(ctx, q).Result()
	if err == redis.Nil {
		// we want call external data source
		escapedQ := url.PathEscape(q)
		address := fmt.Sprintf("https://nominatim.openstreetmap.org/search?q=%s&format=json", escapedQ)

		resp, err := http.Get(address)
		if err != nil {
			return nil, false, err
		}

		data := make([]NominatimResponse, 0)

		err = json.NewDecoder(resp.Body).Decode(&data)
		if err != nil {
			return nil, false, err
		}

		b, err := json.Marshal(data)
		if err != nil {
			return nil, false, err
		}

		// set the value
		err = a.cache.Set(ctx, q, bytes.NewBuffer(b).Bytes(), time.Second*15).Err()
		if err != nil {
			return nil, false, err
		}

		// return the response
		return data, false, nil
	} else if err != nil {
		fmt.Printf("error calling redis: %v\n", err)
		return nil, false, err
	} else {
		// cache hit
		data := make([]NominatimResponse, 0)

		// build response
		err := json.Unmarshal(bytes.NewBufferString(value).Bytes(), &data)
		if err != nil {
			return nil, false, err
		}

		// return response
		return data, true, nil
	}
}

type API struct {
	cache *redis.Client
}

func NewAPI() *API {
	var opts *redis.Options

	if os.Getenv("LOCAL") == "true" {
		redisAddress := fmt.Sprintf("%s:6379", os.Getenv("REDIS_URL"))
		opts = &redis.Options{
			Addr:     redisAddress,
			Password: "", // no password set
			DB:       0,  // use default DB
		}
	} else {
		builtOpts, err := redis.ParseURL(os.Getenv("REDIS_URL"))
		if err != nil {
			panic(err)
		}
		opts = builtOpts
	}

	rdb := redis.NewClient(opts)

	return &API{
		cache: rdb,
	}
}

type APIResponse struct {
	Cache bool                `json:"cache"`
	Data  []NominatimResponse `json:"data"`
}

type NominatimResponse struct {
	PlaceID     int      `json:"place_id"`
	Licence     string   `json:"licence"`
	OsmType     string   `json:"osm_type"`
	OsmID       int      `json:"osm_id"`
	Boundingbox []string `json:"boundingbox"`
	Lat         string   `json:"lat"`
	Lon         string   `json:"lon"`
	DisplayName string   `json:"display_name"`
	Class       string   `json:"class"`
	Type        string   `json:"type"`
	Importance  float64  `json:"importance"`
	Icon        string   `json:"icon"`
}
