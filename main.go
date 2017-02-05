package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

func main() {

	// list currently "watched" games
	// create a game "watch"

	// find today's games
	// while !EndofGame
	//   get pbp
	//   filter pbp
	//   send to twitter

	// requested game by gamecode

	r := NewRegistrar()
	h := Handler{r}

	http.HandleFunc("/active", h.GetActiveGames)
	http.HandleFunc("/activate/", h.ActivateGame)

	log.Fatal(http.ListenAndServe(":8084", nil))
}

type Handler struct {
	r Registrar
}

func (h *Handler) GetActiveGames(w http.ResponseWriter, r *http.Request) {
	g := struct {
		Games []string `json:"games"`
	}{
		h.r.getActiveGames(),
	}

	enc := json.NewEncoder(w)
	err := enc.Encode(&g)
	if err != nil {
		log.Printf("error encoding active games: %s\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, `{"error":"server error occurred"}`)
		return
	}
}

func (h *Handler) ActivateGame(w http.ResponseWriter, r *http.Request) {
	requestedGameCode := r.URL.Path[len("/activate/"):]

	if _, ok := h.r.m[requestedGameCode]; !ok {
		go activate(&h.r, requestedGameCode)
		log.Printf("now activating %s\n", requestedGameCode)
		w.WriteHeader(http.StatusAccepted)
	} else {
		log.Printf("already activated %s\n", requestedGameCode)
		w.WriteHeader(http.StatusAccepted)
	}
}

type Registrar struct {
	sync.RWMutex
	m map[string]bool
}

func NewRegistrar() Registrar {
	return Registrar{m: make(map[string]bool)}
}

func (r *Registrar) getActiveGames() []string {
	keys := make([]string, 0, len(r.m))
	for k := range r.m {
		keys = append(keys, k)
	}
	return keys
}

func (r *Registrar) markGame(gameCode string) {
	r.Lock()
	r.m[gameCode] = true
	r.Unlock()
}

func (r *Registrar) unmarkGame(gameCode string) {
	r.Lock()
	r.m[gameCode] = true
	r.Unlock()
}

func activate(r *Registrar, gameCode string) error {
	r.markGame(gameCode) // consider short circuit if already watching?
	defer r.unmarkGame(gameCode)

	pbp, err := game(gameCode)
	if err != nil {
		log.Printf("something went wrong retrieving: %s\n", err)
		return err
	}

	for pbp.Game.Active {
		pbp, err = game(gameCode) // better way to do this?
		if err != nil {
			log.Printf("something went wrong retrieving: %s\n", err)
			return err
		}
		pbp, err = filter(pbp)
		if err != nil {
			log.Printf("something went wrong filtering: %s\n", err)
			return err
		}
		err = tweet(pbp)
		if err != nil {
			log.Printf("something went wrong tweeting: %s\n", err)
			return err
		}
		time.Sleep(10 * time.Second) // better way to do this for sure
	}
	return nil
}

func game(gameCode string) (PlayByPlayGame, error) {
	url := fmt.Sprintf("http://localhost:8081/pbp/%s", gameCode)

	resp, err := http.Get(url)
	if err != nil {
		fmt.Println("something went wrong in retrieving pbp", err)
		return PlayByPlayGame{}, err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	var pbp PlayByPlayGame
	for dec.More() {
		err := dec.Decode(&pbp)
		if err != nil {
			log.Printf("error decoding pbp game %s\n", err)
			return PlayByPlayGame{}, err
		}
	}

	return pbp, nil
}

func filter(pbp PlayByPlayGame) (PlayByPlayGame, error) {
	b := new(bytes.Buffer)
	json.NewEncoder(b).Encode(pbp) // TODO
	url := fmt.Sprintf("http://localhost:8082/filter/%s", pbp.Game.GameCode())
	resp, err := http.Post(url, "application/json", b)
	if err != nil {
		log.Printf("error posting raw pbp game %s\nposted to url: %s", err, url)
		return PlayByPlayGame{Game: pbp.Game}, err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	var filteredPbp PlayByPlayGame
	for dec.More() {
		err := dec.Decode(&filteredPbp)
		if err != nil {
			log.Printf("error decoding pbp game %s\n", err)
			return PlayByPlayGame{}, err
		}
	}
	return filteredPbp, nil
}

func tweet(pbp PlayByPlayGame) error {
	b := new(bytes.Buffer)
	json.NewEncoder(b).Encode(pbp)
	url := "http://localhost:8083/tweet"

	_, err := http.Post(url, "application/json", b)

	if err != nil {
		return err
	}
	return nil
}

type PlayByPlayGame struct {
	Game
	Plays []Play
}

type Play struct {
	Clock            string        `json:"clock"`
	Description      string        `json:"description"`
	PersonId         string        `json:"personId"`
	TeamId           string        `json:"teamId"`
	VistingTeamScore string        `json:"vTeamScore"`
	HomeTeamScore    string        `json:"hTeamScore"`
	IsScoreChange    bool          `json:"isScoreChange"`
	Formatted        FormattedPlay `json:"formatted"`
}

type FormattedPlay struct {
	Description string `json:"description"`
}

type Game struct {
	Id           string    `json:"gameId"`
	StartTime    time.Time `json:"startTimeUTC"`
	VisitingTeam Team      `json:"vTeam"`
	HomeTeam     Team      `json:"hTeam"`
	Period       Period    `json:"period"`
	Active       bool      `json:"isGameActivated"`
}

func (g Game) GameCode() string {
	return fmt.Sprintf("%s%s", g.VisitingTeam.TriCode, g.HomeTeam.TriCode)
}

// GameDate returns the start date of game (YYYYMMDD format) in US/Eastern tz
// TODO: make sure output is in eastern
func (g Game) GameDate() string {
	easternTime, err := time.LoadLocation("America/New_York")
	if err != nil {
		os.Exit(1)
	}
	return g.StartTime.In(easternTime).Format("20060102")
}

type Team struct {
	Id      string `json:"teamId"`
	TriCode string `json:"triCode"`
}

type Period struct {
	Current int
}
