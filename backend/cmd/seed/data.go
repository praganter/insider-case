package main

import (
	"fmt"
	"math/rand/v2"
	"time"
)

const userCount = 5000

type user struct {
	ID                       int64
	FirstName, LastName      string
	Email, Phone             string
	City, Language, Gender   string
	BirthYear                int
	ConsentSMS, ConsentEmail bool
	CreatedAt                time.Time
}

type event struct {
	UserID int64
	Type   string
	At     time.Time
	Amount *float64
}

type weighted struct {
	v string
	w int
}

var cities = []weighted{
	{"İstanbul", 30}, {"Ankara", 15}, {"İzmir", 12}, {"Bursa", 8}, {"Antalya", 8},
	{"Adana", 6}, {"Konya", 5}, {"Gaziantep", 5}, {"Eskişehir", 5}, {"Trabzon", 6},
}
var languages = []weighted{{"tr", 85}, {"en", 15}}
var genders = []weighted{{"female", 48}, {"male", 48}, {"other", 4}}
var eventTypes = []weighted{{"page_view", 60}, {"cart_add", 25}, {"purchase", 15}}

var firstNames = []string{"Ayşe", "Mehmet", "Zeynep", "Ahmet", "Elif", "Mustafa", "Defne", "Emre", "Selin", "Can", "Deniz", "Burak"}
var lastNames = []string{"Yılmaz", "Kaya", "Demir", "Şahin", "Çelik", "Yıldız", "Aydın", "Özdemir", "Arslan", "Doğan"}

func pick(r *rand.Rand, list []weighted) string {
	total := 0
	for _, e := range list {
		total += e.w
	}
	n := r.IntN(total)
	for _, e := range list {
		if n < e.w {
			return e.v
		}
		n -= e.w
	}
	return list[0].v
}

// generate is deterministic for a given now: the same seed always yields the
// same users and events, so tests can assert known counts.
func generate(now time.Time) ([]user, []event) {
	r := rand.New(rand.NewPCG(42, 42))
	users := make([]user, 0, userCount)
	var events []event
	for i := 1; i <= userCount; i++ {
		first, last := firstNames[r.IntN(len(firstNames))], lastNames[r.IntN(len(lastNames))]
		users = append(users, user{
			ID:           int64(i),
			FirstName:    first,
			LastName:     last,
			Email:        fmt.Sprintf("user%d@example.com", i),
			Phone:        fmt.Sprintf("+90555%07d", i),
			City:         pick(r, cities),
			Language:     pick(r, languages),
			Gender:       pick(r, genders),
			BirthYear:    1955 + r.IntN(53),
			ConsentSMS:   r.IntN(100) < 70,
			ConsentEmail: r.IntN(100) < 80,
			CreatedAt:    now.Add(-time.Duration(r.IntN(730*24)) * time.Hour),
		})
		for range r.IntN(9) {
			e := event{
				UserID: int64(i),
				Type:   pick(r, eventTypes),
				At:     now.Add(-time.Duration(r.IntN(90*24*3600)+60) * time.Second),
			}
			if e.Type == "purchase" {
				amount := float64(500+r.IntN(50000)) / 100
				e.Amount = &amount
			}
			events = append(events, e)
		}
	}
	return users, events
}
