package main

import (
	"bufio"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	sites = []string{
		"https://www.facebook.com",
		"https://www.twitter.com",
		"https://www.instagram.com",
		"https://www.linkedin.com",
		"https://www.gmail.com",
		"https://www.outlook.com",
		"https://www.yahoo.com",
		"https://www.youtube.com",
		"https://www.tiktok.com",
		"https://www.reddit.com",
		"https://www.snapchat.com",
		"https://www.pinterest.com",
		"https://www.threads.net",
	}

	client = &http.Client{
		Transport: &http.Transport{
			MaxIdleConns:        200,
			MaxIdleConnsPerHost: 200,
		},
		Timeout: 0,
	}
)

func makeRequest(url string) {
	_, _ = client.Get(url)
}

func worker(speed time.Duration, wg *sync.WaitGroup, stop <-chan struct{}) {
	defer wg.Done()
	for {
		select {
		case <-stop:
			return
		default:
			url := sites[rand.Intn(len(sites))]
			go makeRequest(url)
			fmt.Printf("\033[1;34m[REQUEST] %s\033[0m\n", url)
			if speed > 0 {
				time.Sleep(speed)
			}
		}
	}
}

func main() {
	fmt.Println("\033[1;32m==============================")
	fmt.Println("  Concurrent Traffic Simulator")
	fmt.Println("==============================\033[0m")
	fmt.Println("\033[1;33mINFO:")
	fmt.Println("- Also specific website or IP.")
	fmt.Println("- trafico IP:port or trafico 4rji.com or https://4rji.com")
	fmt.Println("- Workers are goroutines making requests in parallel.")
	fmt.Println("- Default: 100 workers")
	fmt.Println("- Minimum: 1 (one at a time)")
	fmt.Println("- Recommended: 50-200")
	fmt.Println("- Max tested: ~1000 (depends on system)\033[0m\n")

	// Override sites if argument is given
	if len(os.Args) >= 2 {
		target := os.Args[1]
		if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
			target = "http://" + target
		}
		sites = []string{target}
		fmt.Printf("\033[1;35m[INFO] Target override: %s\033[0m\n\n", target)
	}

	rand.Seed(time.Now().UnixNano())
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("\033[1;36m> Session duration in minutes (0 = infinite): \033[0m")
	dStr, _ := reader.ReadString('\n')
	dStr = strings.TrimSpace(dStr)
	if dStr == "" {
		dStr = "0"
	}
	dur, _ := strconv.Atoi(dStr)

	fmt.Print("\033[1;36m> Delay between requests in seconds (0 = no delay): \033[0m")
	sStr, _ := reader.ReadString('\n')
	sStr = strings.TrimSpace(sStr)
	if sStr == "" {
		sStr = "0"
	}
	delay, _ := strconv.ParseFloat(sStr, 64)

	fmt.Print("\033[1;36m> Number of concurrent workers [Default: 100]: \033[0m")
	wStr, _ := reader.ReadString('\n')
	wStr = strings.TrimSpace(wStr)
	if wStr == "" {
		wStr = "100"
	}
	workers, _ := strconv.Atoi(wStr)

	fmt.Printf("\n\033[1;32m[STARTING] Launching %d workers...\033[0m\n\n", workers)

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker(time.Duration(delay*float64(time.Second)), &wg, stop)
	}

	if dur > 0 {
		time.Sleep(time.Duration(dur) * time.Minute)
		close(stop)
	} else {
		select {} // infinite run
	}
	wg.Wait()
}
