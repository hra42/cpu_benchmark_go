package benchmarks

import (
	"bytes"
	"encoding/json"
	"fmt"

	"cpu_bench_go/runner"
)

const (
	JSONSmall  = 100
	JSONMedium = 1_000
	JSONLarge  = 10_000
)

type jsonAddress struct {
	Street  string `json:"street"`
	City    string `json:"city"`
	Country string `json:"country"`
	Zip     int    `json:"zip"`
}

type jsonUser struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Email     string            `json:"email"`
	Active    bool              `json:"active"`
	Score     float64           `json:"score"`
	Tags      []string          `json:"tags"`
	Meta      map[string]string `json:"meta"`
	Addresses []jsonAddress     `json:"addresses"`
}

type JSON struct {
	n          int
	payload    []byte
	expectedID int64
	users      []jsonUser
	out        bytes.Buffer
}

func (j *JSON) Name() string { return "json" }

func (j *JSON) Tags() []string { return []string{"memory", "allocation", "string"} }

func (j *JSON) Setup(n int) {
	if n <= 0 {
		n = JSONMedium
	}
	j.n = n
	users := buildJSONUsers(n)
	buf, err := json.Marshal(users)
	if err != nil {
		panic(err)
	}
	j.payload = buf
	j.expectedID = users[len(users)-1].ID
	j.users = make([]jsonUser, 0, n)
	j.out.Grow(len(buf))
}

func (j *JSON) Run() any {
	j.users = j.users[:0]
	if err := json.Unmarshal(j.payload, &j.users); err != nil {
		return int64(-1)
	}
	j.out.Reset()
	enc := json.NewEncoder(&j.out)
	if err := enc.Encode(j.users); err != nil {
		return int64(-1)
	}
	if len(j.users) == 0 {
		return int64(0)
	}
	return j.users[len(j.users)-1].ID
}

func (j *JSON) Verify(result any) bool {
	got, ok := result.(int64)
	if !ok {
		return false
	}
	return got == j.expectedID
}

func buildJSONUsers(n int) []jsonUser {
	users := make([]jsonUser, n)
	for i := 0; i < n; i++ {
		addrCount := 1 + i%3
		addrs := make([]jsonAddress, addrCount)
		for k := 0; k < addrCount; k++ {
			addrs[k] = jsonAddress{
				Street:  fmt.Sprintf("%d Main St", (i+k)*7%1000),
				City:    fmt.Sprintf("City%d", (i+k)%50),
				Country: "DE",
				Zip:     10000 + (i+k)%90000,
			}
		}
		users[i] = jsonUser{
			ID:     int64(i + 1),
			Name:   fmt.Sprintf("User %d", i),
			Email:  fmt.Sprintf("user%d@example.com", i),
			Active: i%2 == 0,
			Score:  float64(i%100) * 0.5,
			Tags:   []string{"alpha", "beta", fmt.Sprintf("tag%d", i%17)},
			Meta: map[string]string{
				"role":   fmt.Sprintf("role%d", i%5),
				"region": fmt.Sprintf("r%d", i%4),
			},
			Addresses: addrs,
		}
	}
	return users
}

var _ runner.Benchmark = (*JSON)(nil)
