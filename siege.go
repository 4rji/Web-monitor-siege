package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	success       int64
	fail          int64
	totalTime     time.Duration
	totalRequests int64
	mu            sync.Mutex

	client = &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        10000,
			MaxIdleConnsPerHost: 10000,
			DisableKeepAlives:   false,
		},
		Timeout: 5 * time.Second,
	}
)

func worker(wg *sync.WaitGroup, url string, stop <-chan struct{}) {
	defer wg.Done()
	for {
		select {
		case <-stop:
			return
		default:
			start := time.Now()
			resp, err := client.Get(url)
			duration := time.Since(start)

			mu.Lock()
			totalRequests++
			totalTime += duration
			if err != nil || resp.StatusCode >= 400 {
				fail++
			} else {
				success++
			}
			mu.Unlock()

			if resp != nil {
				resp.Body.Close()
			}
		}
	}
}

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: go run main.go <url> <concurrency> <duration_minutes>")
		return
	}

	url := os.Args[1]
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}

	concurrency, _ := strconv.Atoi(os.Args[2])
	durationMin, _ := strconv.Atoi(os.Args[3])
	duration := time.Duration(durationMin) * time.Minute

	fmt.Printf("[INFO] Target: %s\n", url)
	fmt.Printf("[INFO] Concurrency: %d\n", concurrency)
	fmt.Printf("[INFO] Duration: %v\n\n", duration)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go worker(&wg, url, stop)
	}

	time.Sleep(duration)
	close(stop)
	wg.Wait()

	avgTime := time.Duration(0)
	if totalRequests > 0 {
		avgTime = totalTime / time.Duration(totalRequests)
	}
	availability := float64(success) / float64(totalRequests) * 100

	fmt.Println("\n========= Siege Report =========")
	fmt.Printf("Transactions:\t\t%d hits\n", totalRequests)
	fmt.Printf("Availability:\t\t%.2f %%\n", availability)
	fmt.Printf("Elapsed time:\t\t%.2f secs\n", duration.Seconds())
	fmt.Printf("Response time:\t\t%.2f secs\n", avgTime.Seconds())
	fmt.Printf("Transaction rate:\t%.2f trans/sec\n", float64(totalRequests)/duration.Seconds())
	fmt.Printf("Successful transactions:\t%d\n", success)
	fmt.Printf("Failed transactions:\t\t%d\n", fail)
}
