package main

import (
	"bufio"
	"context"
	"database/sql"
	tunnel "disenroll-inverter/src"
	"disenroll-inverter/src/dbops"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"time"

	// Third party packages
	"github.com/joho/godotenv"

	_ "github.com/go-sql-driver/mysql"
)

func main(){

	// Load environment variables from .env file
	err := godotenv.Load()
	if err != nil {
		log.Fatal("error loading .env file")
	}

	sshUser := os.Getenv("SSH_USER")
	sshPrivateKey := os.Getenv("SSH_PRIVATE_KEY_PATH")
	sshDestination := os.Getenv("SSH_DESTINATION")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Printf("Hey Manuel, this program will:\n")
	fmt.Printf("  * check if inverter is enrolled\n")
	fmt.Printf("  * delete record if it is\n")
	fmt.Printf("  * check if inverter exists\n")
	fmt.Printf("  * delete record if it does\n")
	fmt.Printf("  * delete record from AWS DynamoDB cache\n\n\n")

	fmt.Printf("Getting $HOME directory...")
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("\nError while getting home directory. Here's why: %v", err)
	}
	fmt.Printf("Done\n")

	// Start SSH Tunnel
	// Setup tunnel, do not start it yet
	sshTunnel := tunnel.NewSSHTunnel(
		sshUser, // user@host, default port is 22 if not specified
		tunnel.PrivateKeyFile(homeDir+sshPrivateKey), // Auth via private key
		sshDestination,
	)

	// Start SSH Tunnel in its goroutine, use context for handling shutdown.
	fmt.Printf("Starting SSH Tunnel...")
	go func() {
		if err := sshTunnel.Start(ctx); err != nil {
			log.Fatalf("\nFailed to start SSH tunnel. Here's why: %v", err)
		}
	}()
	fmt.Printf("Done\n")

	// Wait for tunnel to signal readiness.
	if err := sshTunnel.WaitReady(ctx); err != nil {
		log.Fatalf("\nError waiting for tunnel. Here's why: %v", err)
	}
	fmt.Printf("Tunnel established successfully on %s.\n", sshTunnel.Local)

	// Open HeidiSQL Connection
	dbName := os.Getenv("DBNAME")
	dbUsername := os.Getenv("DB_USERNAME")
	dbPassword := os.Getenv("DB_PASSWORD")
	connStr := fmt.Sprintf("%s:%s@tcp(127.0.0.1:%d)/%s", dbUsername, dbPassword, sshTunnel.Local.Port, dbName)

	fmt.Printf("Connecting to HeidiSQL...")
	db, err := sql.Open("mysql", connStr)
	if err != nil {
		log.Fatalf("failed to open database. Here's why: %v", err)
	}
	defer db.Close()

	// Explicit ping timeout to bound waiting.
	ctxPing, cancelPing := context.WithTimeout(ctx, 5 * time.Second)
	defer cancelPing()
	if err := db.PingContext(ctxPing); err != nil {
		log.Fatalf("\nFailed to ping database: %v", err)
	}
	fmt.Printf("Done\n")

	// Start a watchdog that pings DB every 30 second, making sure connection keeps alive.
	go dbops.StartWatchdog(ctx, db, 30*time.Second)

	// Init inverter struct before user input loop.
	inverter := &dbops.Inverter{}
	heidiTable := os.Getenv("HEIDITABLE1")

	for {
		fmt.Printf("\nType Inverter MAC Address: ")
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			log.Fatalf("\nfailed to read selection")
			return
		}

		inverter.MAC = scanner.Text()

		// TODO: make a list of available tables
		fmt.Printf("\nChecking if %s exists in heidiSQL table...", inverter.MAC)
		inverterExists := inverter.CheckIfExists(db, heidiTable)
		if inverterExists {
			fmt.Printf("Done [1/6]")
			fmt.Printf("\nInverter is enrolled.")
			break
		}else {
			fmt.Printf("\nInverter is not enrolled.")
		}
	}

	fmt.Printf("\nDeleting %s from table...", inverter.MAC)
	err = inverter.DeleteMacFromTable(db, "enrollment")
	if err != nil {
		fmt.Printf("\nDelete operation error: %v", err)
	}	
	fmt.Printf("Done [2/6]")

	heidiTable = os.Getenv("HEIDITABLE2")
	fmt.Printf("\nChecking if %s exists in heidiSQL table...", inverter.MAC)
	inverterExists := inverter.CheckIfExists(db, heidiTable)
	if inverterExists {
		fmt.Printf("\nInverter exists.")
		fmt.Printf("Done [3/6]")
	}else {
		fmt.Printf("\nInverter does not exist in table.")
		ctx.Done()
	}

	fmt.Printf("\nDeleting %s from table...", inverter.MAC)
	err = inverter.DeleteMacFromTable(db, "appliance")
	if err != nil {
		fmt.Printf("\nDelete operation error: %v", err)
	}	
	fmt.Printf("Done [4/6]")

	dynamoClient := dbops.InitDynamoClient()

	dynamoCache := os.Getenv("AWSDYNAMOTABLE1")
	fmt.Printf("\nDeleting %s from cache...", inverter.MAC)
	err = inverter.DeleteMacFromDynamoDBTable(ctx, dynamoClient, dynamoCache)
	if err != nil {
		fmt.Printf("\nDelete operation error: %v", err)
	}	
	fmt.Printf("Done [5/6]")

	dynamoCache = os.Getenv("AWSDYNAMOTABLE2")
	fmt.Printf("\nDeleting %s from cache...", inverter.MAC)
	err = inverter.DeleteMacFromDynamoDBTable(ctx, dynamoClient, dynamoCache)
	if err != nil {
		fmt.Printf("\nDelete operation error: %v", err)
	}	
	fmt.Printf("Done [6/6]")

	// Block main until signal of shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case <- ctx.Done():
		log.Println("context canceled, shutting down...")
	case s := <- sigCh:
		log.Printf("\nReceived signal %s, shutting down...", s)
		cancel() // shut down context
	}
}
