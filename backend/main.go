package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

var validID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

type app struct {
	repo   repository
	redis  *redis.Client
	secret []byte
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func main() {
	merchantJev, err := newMerchantJev(os.Getenv("PAYGUARD_AI_MODE"), os.Getenv("OPENROUTER_API_KEY"), os.Getenv("OPENROUTER_MODEL_PRIMARY"))
	if err != nil {
		log.Fatal(err)
	}
	var baselineMinSamples int
	if raw := os.Getenv("PAYGUARD_DEMO_BASELINE_MIN_SAMPLES"); raw != "" {
		baselineMinSamples, err = strconv.Atoi(raw)
		if err != nil || baselineMinSamples < 1 {
			log.Fatal("PAYGUARD_DEMO_BASELINE_MIN_SAMPLES must be a positive integer")
		}
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, env("DATABASE_URL", "postgres://payguard:payguard@127.0.0.1:55432/payguard?sslmode=disable"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := initialize(ctx, db); err != nil {
		log.Fatal(err)
	}
	production, err := newProductionAuth(ctx, db)
	if err != nil {
		log.Fatal(err)
	}
	demoCredential := func(key string) string {
		if production != nil {
			return ""
		}
		return os.Getenv(key)
	}
	bookingAuth, err := newBookingAuth(demoCredential("PAYGUARD_DEMO_MERCHANT_A_TOKEN"), demoCredential("PAYGUARD_DEMO_MERCHANT_B_TOKEN"),
		bookingActorCredential{"demo-merchant-a", "analyst", bookingRoleAnalyst, demoCredential("PAYGUARD_DEMO_ANALYST_A_TOKEN")},
		bookingActorCredential{"demo-merchant-a", "analyst-2", bookingRoleAnalyst, demoCredential("PAYGUARD_DEMO_ANALYST_A2_TOKEN")},
		bookingActorCredential{"demo-merchant-a", "viewer", bookingRoleViewer, demoCredential("PAYGUARD_DEMO_VIEWER_A_TOKEN")},
		bookingActorCredential{"demo-merchant-b", "analyst", bookingRoleAnalyst, demoCredential("PAYGUARD_DEMO_ANALYST_B_TOKEN")},
		bookingActorCredential{"demo-merchant-b", "viewer", bookingRoleViewer, demoCredential("PAYGUARD_DEMO_VIEWER_B_TOKEN")},
	)
	if err != nil {
		log.Fatal(err)
	}
	options, err := redis.ParseURL(env("REDIS_URL", "redis://127.0.0.1:56379/0"))
	if err != nil {
		log.Fatal(err)
	}
	cache := redis.NewClient(options)
	defer cache.Close()
	enrichment, err := newEnrichmentHandler(bookingAuth, db, options)
	if err != nil {
		log.Fatal(err)
	}
	if enrichment.cache != nil {
		defer enrichment.cache.Close()
	}
	a := app{repository{db}, cache, []byte(env("ENTITY_KEY_SECRET", "payguard-local-synthetic-v1"))}
	checkout := newOmiseCheckout(a.repo, os.Getenv("OMISE_PUBLIC_KEY"), os.Getenv("OMISE_SECRET_KEY"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.health)
	mux.HandleFunc("POST /api/v1/booking/events", bookingHandler{a.repo, bookingAuth}.create)
	bookingRisk := bookingRiskHandler{repo: a.repo, auth: bookingAuth, redis: cache, baselineMinSamples: baselineMinSamples}
	mux.HandleFunc("POST /api/v1/booking/profiles/proposal", bookingRisk.proposeProfile)
	mux.HandleFunc("POST /api/v1/booking/profiles/approve", bookingRisk.approveProfile)
	mux.HandleFunc("GET /api/v1/booking/jev/rules", bookingRisk.listJevRules)
	mux.HandleFunc("POST /api/v1/booking/jev/rules", bookingRisk.pushJevRule)
	mux.HandleFunc("POST /api/v1/booking/jev/rules/preview", bookingRisk.testJevRule)
	mux.HandleFunc("POST /api/v1/booking/events/{id}/assessment", bookingRisk.assessEvent)
	mux.HandleFunc("POST /api/v1/booking/events/{id}/outcomes", bookingRisk.importOutcome)
	mux.HandleFunc("GET /api/v1/booking/events/{id}/lineage", bookingRisk.readLineage)
	mux.HandleFunc("POST /api/v1/booking/shadow", bookingRisk.shadowBookings)
	registerBookingOps(mux, a.repo, bookingAuth, a.secret)
	adminToken := demoCredential("PAYGUARD_DEMO_ADMIN_TOKEN")
	registerMerchantDemo(mux, a.repo, adminToken, merchantJev)
	registerRuleComposer(mux, a, adminToken, merchantJev)
	registerJevAIConfiguration(mux, a.repo, adminToken, merchantJev)
	registerPlatformRuleGovernance(mux, a.repo, adminToken, demoCredential("PAYGUARD_PLATFORM_RULE_CHECKER_TOKEN"))
	registerBookingDossiers(mux, a.repo, bookingAuth)
	registerBookingGovernance(mux, a.repo, bookingAuth)
	go a.repo.runBookingDossiers(ctx)
	mux.HandleFunc("POST /api/v1/enrichment", enrichment.lookup)
	mux.HandleFunc("POST /api/v1/simulation/runs", productionRoles(a.create, productionRoleAdmin))
	mux.HandleFunc("GET /api/v1/simulation/runs/{id}", productionRoles(a.readRun, productionRoleAdmin))
	mux.HandleFunc("GET /api/v1/simulation/runs/{id}/report", productionRoles(a.readReport, productionRoleAdmin))
	a.registerTenantSimulation(mux, bookingAuth)
	mux.HandleFunc("GET /api/v1/simulation/checkout/config", checkout.config)
	mux.HandleFunc("POST /api/v1/simulation/checkout/charges", checkout.create)
	mux.HandleFunc("GET /api/v1/system/rules", productionRoles(a.readRules, productionRoleAdmin, productionRoleChecker))
	mux.HandleFunc("POST /api/v1/system/rules", platformRuleWrite(a.repo, adminToken, a.createRule))
	mux.HandleFunc("DELETE /api/v1/system/rules/{id}", platformRuleWrite(a.repo, adminToken, a.deleteRule))
	mux.HandleFunc("PUT /api/v1/system/rules/{id}", platformRuleWrite(a.repo, adminToken, a.updateRule))
	mux.HandleFunc("POST /api/v1/system/rules/preview", platformRuleWrite(a.repo, adminToken, a.previewRules))
	var handler http.Handler = mux
	if production != nil {
		production.register(mux)
		handler = production.protect(mux)
	}
	srv := &http.Server{
		Addr: env("PAYGUARD_ADDR", "127.0.0.1:8081"), Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: time.Minute,
	}
	log.Printf("Simulation API listening on %s", srv.Addr)
	log.Fatal(srv.ListenAndServe())
}

func respond(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func problem(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}

func unavailable(w http.ResponseWriter, err error) {
	log.Printf("simulation dependency error: %v", err)
	problem(w, 503, "Simulation storage is temporarily unavailable. Please retry.")
}

func (a app) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := a.repo.db.Ping(ctx); err != nil {
		unavailable(w, err)
		return
	}
	if err := a.redis.Ping(ctx).Err(); err != nil {
		unavailable(w, err)
		return
	}
	respond(w, 200, map[string]string{"status": "ready"})
}

func (a app) create(w http.ResponseWriter, r *http.Request) {
	a.createSimulation(w, r, merchantID)
}

func (a app) createSimulation(w http.ResponseWriter, r *http.Request, tenant string) {
	var request struct {
		ID     string `json:"id"`
		Config config `json:"config"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || !validID.MatchString(request.ID) {
		problem(w, 400, "Expected a run UUID and simulation configuration.")
		return
	}
	request.Config.ContactEmailDomain = strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + ('a' - 'A')
		}
		return r
	}, strings.TrimSpace(request.Config.ContactEmailDomain))
	if err := request.Config.validate(); err != nil {
		problem(w, 422, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	saved, err := a.repo.get(ctx, tenant, request.ID)
	switch {
	case err == nil:
		returnRun(w, saved, request.Config)
		return
	case !errors.Is(err, pgx.ErrNoRows):
		unavailable(w, err)
		return
	}

	id, err := attemptID()
	if err != nil {
		unavailable(w, err)
		return
	}
	platformRules, err := a.repo.activePlatformRules(ctx)
	if err != nil {
		unavailable(w, err)
		return
	}
	events, truth := generate(request.Config, tenant, a.secret)
	counter := redisVelocity{a.redis, "payguard:" + tenant + ":" + request.ID + ":" + id}
	decisions, err := evaluate(ctx, counter, events, request.Config.MerchantPolicy, request.Config.JevTimeout)
	if err != nil {
		unavailable(w, err)
		return
	}
	for i := range decisions {
		applyGovernedPlatformRules(&decisions[i], platformRules)
	}
	result := run{request.ID, request.Config, generatorVersion, platformRulesetVersion(platformRules), jevVersion, len(events) - len(decisions), decisions}
	saved, err = a.repo.save(ctx, tenant, result, truth)
	if err != nil {
		unavailable(w, err)
		return
	}
	returnRun(w, saved, request.Config)
}

func returnRun(w http.ResponseWriter, saved run, requested config) {
	if saved.Config != requested {
		problem(w, 409, "Run ID already exists with a different configuration.")
		return
	}
	respond(w, 200, saved)
}

func (a app) find(w http.ResponseWriter, r *http.Request, tenant string) (run, bool) {
	id := r.PathValue("id")
	if !validID.MatchString(id) {
		problem(w, 404, "Run not found.")
		return run{}, false
	}
	result, err := a.repo.get(r.Context(), tenant, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		problem(w, 404, "Run not found.")
	case err != nil:
		unavailable(w, err)
	default:
		return result, true
	}
	return run{}, false
}

func (a app) readRun(w http.ResponseWriter, r *http.Request) {
	if saved, ok := a.find(w, r, merchantID); ok {
		respond(w, 200, saved)
	}
}

func (a app) readReport(w http.ResponseWriter, r *http.Request) {
	a.readSimulationReport(w, r, merchantID)
}

func (a app) readSimulationReport(w http.ResponseWriter, r *http.Request, tenant string) {
	saved, ok := a.find(w, r, tenant)
	if !ok {
		return
	}
	result, err := a.repo.qaReport(r.Context(), tenant, saved)
	if err != nil {
		unavailable(w, err)
		return
	}
	respond(w, 200, result)
}
