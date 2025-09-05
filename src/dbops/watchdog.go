package dbops

import (
	"context"
	"database/sql"
	"log"
	"time"
)

// StartWatchdog pings the given sql database, every given `interval`.
func StartWatchdog(ctx context.Context, db *sql.DB, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {

		// See https://blog.golang.org/pipelines for more examples of how to use
		// a Done channel for cancellation.
		case <- ctx.Done():
			log.Println("Database watchdog shutting down.")
			return
		case <- ticker.C:
			// Use a short timeout for ping
			pingCtx, cancel := context.WithTimeout(ctx, time.Second * 5)
			defer cancel()

			// PingContext verifies if a connection to the database is still alive,
			// establishing a new connection if necessary.
			if err := db.PingContext(pingCtx); err != nil {
				log.Printf("\nWatchdog: DB connection unhealthy. Here's why: %v", err)
			}
		}
	}
}
