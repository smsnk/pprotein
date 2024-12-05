package main

import (
	"os"

	"github.com/smsnk/pprotein/integration/standalone"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "19000"
	}
	standalone.Integrate(":" + port)
}
