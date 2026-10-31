// Command worker delivers queued messages through the provider.
package main

import (
	"context"
	"log"
	"os"
	"strconv"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/praganter/insider-case/backend/internal/campaign"
	"github.com/praganter/insider-case/backend/internal/sender"
	"github.com/praganter/insider-case/backend/internal/store"
)

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func main() {
	st, err := store.New(context.Background(), os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	redisAddr := env("REDIS_ADDR", "localhost:6379")
	rate, _ := strconv.Atoi(env("RATE_PER_SEC", "100"))

	h := &sender.Handler{
		Store:    st,
		Provider: sender.NewProvider(env("PROVIDER_URL", "http://localhost:9090")),
		// ponytail: burst 1 spaces messages evenly; raise it only if the provider tolerates bursts.
		Limiter: &sender.Limiter{Redis: redis.NewClient(&redis.Options{Addr: redisAddr}), Rate: rate, Burst: 1},
	}
	srv := asynq.NewServer(asynq.RedisClientOpt{Addr: redisAddr}, asynq.Config{
		Concurrency: 20,
		// One queue per channel (see NewEnqueuer); default carries the scheduled campaign tasks.
		Queues:         map[string]int{"sms": 1, "email": 1, "default": 1},
		RetryDelayFunc: sender.Backoff,
	})
	mux := asynq.NewServeMux()
	mux.HandleFunc(sender.TaskType, h.Process)

	// A scheduled campaign fans out and queues its messages from here, so the
	// worker needs the queue client the API uses too.
	queue := asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})
	defer queue.Close()
	mux.HandleFunc(sender.CampaignTaskType, sender.CampaignHandler(&campaign.Service{Store: st, Enqueue: sender.NewEnqueuer(queue)}))
	log.Printf("worker started (rate %d/s per channel)", rate)
	log.Fatal(srv.Run(mux))
}
