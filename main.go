package main

import (
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
)

func main(){
	err := godotenv.Load()
	if err != nil {
		log.Fatal("error loading .env file")
	}

	sshUser := os.Getenv("SSH_USER")
	sshPrivateKey := os.Getenv("SSH_PRIVATE_KEY_PATH")
	sshDestination := os.Getenv("SSH_DESTINATION")

}

