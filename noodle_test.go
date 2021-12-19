package noodle

import (
	"fmt"
	"log"
	"math/rand"
	"testing"

	"sync"
)

func TestFull(t *testing.T) {
	var c Config
	c.InsecureNoAuthenticateHandshake = true
	nc, err := Listen("127.0.0.1:3322", &c)
	if err != nil {
		t.Fatal(err)
	}

	go func() {

		for c := range nc {

			go func(conn *Connection) {

				buf := make([]byte, 64)
				defer conn.Close()

				for {
					n, err := conn.Read(buf)
					if err != nil {
						return
					}

					_, err = conn.Write([]byte(fmt.Sprintf("Echo: %s", buf[:n])))
					if err != nil {
						log.Println("Server write:", err)
						return
					}
				}

			}(c)
		}
	}()

	var wg sync.WaitGroup
	for x := 0; x < 5000; x++ {
		wg.Add(1)
		go func(no int) {
			defer wg.Done()
			client, _, err := DialWithConfig("127.0.0.1:3322", &c)
			if err != nil {
				log.Fatal(err)
			}
			defer client.Close()

			buf := make([]byte, 64)
			m := rand.Intn(200)
			for i := 0; i < m; i++ {

				_, err := client.Write([]byte("Test"))
				if err != nil {
					t.Fatal("Client write: ", err)
				}

				_, err = client.Read(buf)
				if err != nil {
					t.Fatal("Client read: ", err)
				}

			}

			fmt.Println("Ended ", no)
		}(x)
	}

	wg.Wait()
}
