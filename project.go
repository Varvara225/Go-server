package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/time/rate"
)

type Client struct {
	TotalRequests int         `json:"total_requests"`
	StatusCounts  map[int]int `json:"status_counts"`
}

type requestStats struct {
	mu            sync.Mutex
	TotalRequests int             `json:"total_requests"`
	StatusCounts  map[int]int     `json:"status_counts"`
	ClientStats   map[int]*Client `json:"client_stats"`
}

var (
	serverStats = requestStats{
		TotalRequests: 0,
		StatusCounts:  make(map[int]int),
		ClientStats:   make(map[int]*Client),
	}
	limiter     = rate.NewLimiter(rate.Every(time.Second/5), 5)
	ctx, cancel = context.WithCancel(context.Background())
)

func main() {
	// загрузка .env file
	err := godotenv.Load("/Users/Varvara/Downloads/WBTypes5/WBTypes/Project/.env")
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}

	envPort := os.Getenv("SERVER_PORT")
	if envPort == "" {
		envPort = "8080"
	}

	http.HandleFunc("/", handleRequest)
	http.HandleFunc("/stats", handleStats)

	fmt.Printf("Server is running on port %s\n", envPort)
	go func() {
		if err := http.ListenAndServe(":"+envPort, nil); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	var wg sync.WaitGroup
	wg.Add(3)

	go client(1, &wg, envPort, ctx)
	go client(2, &wg, envPort, ctx)
	go client(3, &wg, envPort, ctx)

	wg.Wait()
	cancel()

	stats, _ := json.MarshalIndent(serverStats, "", "  ")
	_ = os.WriteFile("server_stats.json", stats, 0644)
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
	// пропускная способность сервера
	if !limiter.Allow() {
		http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	switch r.Method {
	case "GET":
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("GET request processed"))
	case "POST":
		w.Write([]byte("POST request processed"))
	default:
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
	}
}

func handleStats(w http.ResponseWriter, r *http.Request) {
	serverStats.mu.Lock()
	defer serverStats.mu.Unlock()

	totalRequests := 0
	statusCounts := make(map[int]int)

	for _, client := range serverStats.ClientStats {
		totalRequests += client.TotalRequests
		for status, count := range client.StatusCounts {
			statusCounts[status] += count
		}
	}

	serverStats.TotalRequests = totalRequests
	serverStats.StatusCounts = statusCounts
	json.NewEncoder(w).Encode(serverStats)
}

func client(id int, wg *sync.WaitGroup, port string, ctx context.Context) {
	defer wg.Done()
	if id == 3 {
		for {
			select {
			case <-time.After(5 * time.Second):
				resp, err := http.Get("http://localhost:" + port)
				if err != nil {
					log.Printf("Client %d: Server is unreachable\n", id)
				} else {
					log.Printf("Client %d: Server is reachable\n", id)
					resp.Body.Close()
				}
			case <-ctx.Done():
				return
			}
		}
	} else {
		// Общий лимитер для всех воркеров клиента
		clientLimiter := rate.NewLimiter(rate.Every(time.Second/5), 5)

		var wgWorkers sync.WaitGroup
		wgWorkers.Add(2)
		for i := 1; i <= 2; i++ {
			go worker(i, id, port, &wgWorkers, clientLimiter)
		}
		wgWorkers.Wait()
		log.Printf("Client %d finished", id)
	}
}

func worker(workerID int, clientID int, port string, wg *sync.WaitGroup, limiter *rate.Limiter) {
	defer wg.Done()
	statuses := generateStatuses()

	for i := 1; i <= 50; i++ {
		if err := limiter.Wait(context.Background()); err != nil {
			log.Printf("Worker %d: Client %d: Rate limiter error: %v\n", workerID, clientID, err)
			continue
		}

		resp, err := http.Post("http://localhost:"+port, "application/json", nil)
		if err != nil {
			log.Printf("Worker %d: Client %d: Error making request: %s\n", workerID, clientID, err)
			continue
		}

		serverStats.mu.Lock()
		if _, exists := serverStats.ClientStats[clientID]; !exists {
			serverStats.ClientStats[clientID] = &Client{
				TotalRequests: 0,
				StatusCounts:  make(map[int]int),
			}
		}
		serverStats.ClientStats[clientID].TotalRequests++
		serverStats.ClientStats[clientID].StatusCounts[statuses[i]]++
		serverStats.mu.Unlock()
		resp.Body.Close()
	}
}

func generateStatuses() []int {
	statuses := make([]int, 100)
	for i := 0; i < 70; i++ {
		if i%2 == 0 {
			statuses[i] = http.StatusOK
		} else {
			statuses[i] = http.StatusAccepted
		}
	}
	for i := 70; i < 100; i++ {
		if i%2 == 0 {
			statuses[i] = http.StatusBadRequest
		} else {
			statuses[i] = http.StatusInternalServerError
		}
	}
	rand.Shuffle(len(statuses), func(i, j int) {
		statuses[i], statuses[j] = statuses[j], statuses[i]
	})
	return statuses
}
