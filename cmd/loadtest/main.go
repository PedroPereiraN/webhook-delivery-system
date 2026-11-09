// Teste de carga simples: vários usuários com emails falsos reservando e
// devolvendo tickets ao mesmo tempo. No fim espera o worker esvaziar o outbox
// e confere se cada evento virou exatamente um email no mock.
//
//	go run ./cmd/loadtest -users 30 -iterations 10
package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "github.com/lib/pq"
)

var firstNames = []string{"ana", "bruno", "carla", "diego", "elisa", "felipe", "gabi", "heitor", "isis", "joao", "lara", "marcos"}
var lastNames = []string{"silva", "souza", "costa", "pereira", "alves", "lima", "rocha", "gomes", "martins", "ribeiro"}

type options struct {
	apiURL     string
	mockURL    string
	dbURL      string
	users      int
	iterations int
	maxQty     int
	returnRate float64
	verify     bool
	waitFor    time.Duration
}

type stats struct {
	reserved     atomic.Int64
	returned     atomic.Int64
	failed       atomic.Int64
	ticketsTotal atomic.Int64

	mu        sync.Mutex
	latencies []time.Duration
}

func (s *stats) observe(d time.Duration) {
	s.mu.Lock()
	s.latencies = append(s.latencies, d)
	s.mu.Unlock()
}

func main() {
	var opts options

	flag.StringVar(&opts.apiURL, "api", "http://localhost:8080", "URL da api")
	flag.StringVar(&opts.mockURL, "mock", "http://localhost:8081", "URL do email-mock")
	flag.StringVar(&opts.dbURL, "db", "postgres://postgres:postgres@localhost:5432/db?sslmode=disable", "Postgres, usado na verificação")
	flag.IntVar(&opts.users, "users", 20, "usuários simultâneos")
	flag.IntVar(&opts.iterations, "iterations", 10, "reservas por usuário")
	flag.IntVar(&opts.maxQty, "max-qty", 3, "máximo de tickets por reserva")
	flag.Float64Var(&opts.returnRate, "return-rate", 0.5, "chance de devolver os tickets de uma reserva")
	flag.BoolVar(&opts.verify, "verify", true, "esperar o worker e conferir outbox x mock")
	flag.DurationVar(&opts.waitFor, "wait", 10*time.Minute, "tempo máximo esperando o worker")
	flag.Parse()

	// Domínio único por execução: a verificação só olha os eventos deste
	// teste, mesmo com dados de execuções anteriores no banco e no mock.
	runID := fmt.Sprintf("run%d", time.Now().Unix())
	domain := runID + ".example.com"

	client := &http.Client{Timeout: 30 * time.Second}
	st := &stats{}

	log.Printf("%d usuários x %d reservas, emails @%s", opts.users, opts.iterations, domain)

	started := time.Now()

	var wg sync.WaitGroup

	for user := range opts.users {
		email := fmt.Sprintf("%s.%s%d@%s",
			firstNames[rand.IntN(len(firstNames))],
			lastNames[rand.IntN(len(lastNames))],
			user, domain,
		)

		wg.Go(func() {
			for range opts.iterations {
				runIteration(client, opts, email, st)
			}
		})
	}

	wg.Wait()

	elapsed := time.Since(started)

	fmt.Println()
	fmt.Printf("Carga (%s)\n", elapsed.Round(time.Millisecond))
	fmt.Printf("  reservas ok:        %d (%d tickets)\n", st.reserved.Load(), st.ticketsTotal.Load())
	fmt.Printf("  devoluções ok:      %d\n", st.returned.Load())
	fmt.Printf("  erros:              %d\n", st.failed.Load())
	printLatencies(st.latencies)

	if !opts.verify {
		return
	}

	// Cada reserva e cada devolução ok gera exatamente um evento: a
	// devolução é sempre de tickets de um único email.
	expected := int(st.reserved.Load() + st.returned.Load())

	if !verify(opts, domain, expected) {
		os.Exit(1)
	}
}

func runIteration(client *http.Client, opts options, email string, st *stats) {
	qty := 1 + rand.IntN(opts.maxQty)

	var reserve struct {
		Tickets []string `json:"tickets"`
	}

	status, err := send(client, st, http.MethodPost, opts.apiURL+"/tickets",
		map[string]any{"email": email, "quantity": qty}, &reserve)

	switch {
	case err != nil:
		st.failed.Add(1)
		log.Printf("reserva falhou: %v", err)
		return
	case status != http.StatusOK:
		st.failed.Add(1)
		log.Printf("reserva respondeu %d", status)
		return
	}

	st.reserved.Add(1)
	st.ticketsTotal.Add(int64(len(reserve.Tickets)))

	if rand.Float64() >= opts.returnRate {
		return
	}

	// Devolve uma parte aleatória (pelo menos um) dos tickets da reserva.
	rand.Shuffle(len(reserve.Tickets), func(i, j int) {
		reserve.Tickets[i], reserve.Tickets[j] = reserve.Tickets[j], reserve.Tickets[i]
	})
	toReturn := reserve.Tickets[:1+rand.IntN(len(reserve.Tickets))]

	status, err = send(client, st, http.MethodDelete, opts.apiURL+"/tickets",
		map[string]any{"ids": toReturn}, nil)

	if err != nil || status != http.StatusOK {
		st.failed.Add(1)
		log.Printf("devolução falhou: status=%d err=%v", status, err)
		return
	}

	st.returned.Add(1)
}

func send(client *http.Client, st *stats, method, url string, body any, out any) (int, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}

	request, err := http.NewRequest(method, url, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}

	request.Header.Set("Content-Type", "application/json")

	start := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()

	st.observe(time.Since(start))

	if out != nil && response.StatusCode == http.StatusOK {
		if err := json.NewDecoder(response.Body).Decode(out); err != nil {
			return response.StatusCode, err
		}
	}

	return response.StatusCode, nil
}

func printLatencies(latencies []time.Duration) {
	if len(latencies) == 0 {
		return
	}

	slices.Sort(latencies)

	percentile := func(p float64) time.Duration {
		return latencies[int(float64(len(latencies)-1)*p)].Round(time.Millisecond)
	}

	fmt.Printf("  latência:           p50=%s p95=%s max=%s (%d requests)\n",
		percentile(0.5), percentile(0.95), latencies[len(latencies)-1].Round(time.Millisecond), len(latencies))
}

// verify espera o outbox não ter mais eventos deste teste em aberto e compara
// com o que o mock recebeu.
func verify(opts options, domain string, expected int) bool {
	db, err := sql.Open("postgres", opts.dbURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	pattern := "%@" + domain

	fmt.Println()
	fmt.Printf("Esperando o worker entregar %d eventos", expected)

	deadline := time.Now().Add(opts.waitFor)

	for {
		var open int

		err := db.QueryRow(
			`SELECT COUNT(*) FROM outbox WHERE email LIKE $1 AND status IN ('pending', 'processing')`,
			pattern,
		).Scan(&open)
		if err != nil {
			log.Fatal(err)
		}

		if open == 0 {
			break
		}

		if time.Now().After(deadline) {
			fmt.Printf("\n  timeout: %d eventos ainda em aberto\n", open)
			break
		}

		fmt.Print(".")
		time.Sleep(2 * time.Second)
	}

	fmt.Println()

	counts := map[string]int{}

	rows, err := db.Query(`SELECT status, COUNT(*) FROM outbox WHERE email LIKE $1 GROUP BY status`, pattern)
	if err != nil {
		log.Fatal(err)
	}

	total := 0

	for rows.Next() {
		var status string
		var count int

		if err := rows.Scan(&status, &count); err != nil {
			log.Fatal(err)
		}

		counts[status] = count
		total += count
	}

	rows.Close()

	var maxAttempts int

	db.QueryRow(`SELECT COALESCE(MAX(attempts), 0) FROM outbox WHERE email LIKE $1`, pattern).Scan(&maxAttempts)

	emails, duplicated, err := mockEmails(opts.mockURL, domain)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Verificação")
	fmt.Printf("  eventos no outbox:  %d (esperado %d)\n", total, expected)
	fmt.Printf("  delivered:          %d\n", counts["delivered"])
	fmt.Printf("  failed:             %d\n", counts["failed"])
	fmt.Printf("  em aberto:          %d\n", counts["pending"]+counts["processing"])
	fmt.Printf("  máx. tentativas:    %d\n", maxAttempts)
	fmt.Printf("  emails no mock:     %d (%d recebidos mais de uma vez, após timeout)\n", emails, duplicated)

	ok := true

	check := func(cond bool, msg string) {
		if !cond {
			ok = false
			fmt.Println("  FALHOU:", msg)
		}
	}

	check(total == expected, "a quantidade de eventos no outbox não bate com as operações feitas")
	check(counts["pending"]+counts["processing"] == 0, "sobraram eventos sem entregar")
	// Falha de entrega também gera email no mock quando o mock "enviou" mas
	// respondeu tarde demais; então emails pode passar de delivered só se
	// houver failed.
	check(emails >= counts["delivered"], "evento marcado como delivered sem email no mock (o mock reiniciou?)")
	check(emails <= counts["delivered"]+counts["failed"], "o mock recebeu email de evento que não existe")

	if ok {
		fmt.Println("  OK")
	}

	return ok
}

// mockEmails conta os emails únicos que o mock recebeu para este teste e
// quantos deles chegaram mais de uma vez.
func mockEmails(mockURL, domain string) (int, int, error) {
	response, err := http.Get(mockURL + "/emails")
	if err != nil {
		return 0, 0, err
	}
	defer response.Body.Close()

	var emails []struct {
		To         string `json:"to"`
		Deliveries int    `json:"deliveries"`
	}

	if err := json.NewDecoder(response.Body).Decode(&emails); err != nil {
		return 0, 0, err
	}

	count, duplicated := 0, 0

	for _, email := range emails {
		if !strings.HasSuffix(email.To, "@"+domain) {
			continue
		}

		count++

		if email.Deliveries > 1 {
			duplicated++
		}
	}

	return count, duplicated, nil
}
