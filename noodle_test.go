package noodle

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"testing"

	"sync"
)

func TestRandom(t *testing.T) {
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
		}(x)
	}

	wg.Wait()
}

type TestConfig struct {
	Test  string
	Toast string
	Size  int
}

func TestJsonDecode(t *testing.T) {
	var c Config
	c.InsecureNoAuthenticateHandshake = true
	nc, err := Listen("127.0.0.1:3323", &c)
	if err != nil {
		t.Fatal(err)
	}

	go func() {

		for c := range nc {

			go func(conn *Connection) {
				defer conn.Close()

				tc := TestConfig{
					Test:  "test",
					Toast: "Noot",
					Size:  123,
				}
				b, _ := json.Marshal(&tc)

				for {
					_, err := conn.Write(b)
					if err != nil {
						t.Fatal(err)
					}

					buf := make([]byte, 100)
					n, err := conn.Read(buf)
					if err != nil {
						t.Fatal(err)
					}

					if string(buf[:n]) != "Live" {
						return
					}

				}

			}(c)
		}
	}()

	client, _, err := DialWithConfig("127.0.0.1:3323", &c)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	dc := json.NewDecoder(client)

	for x := 0; x < 100; x++ {
		var conf TestConfig
		err = dc.Decode(&conf)
		if err != nil {
			t.Fatal(err)
		}

		fmt.Fprintf(client, "Live")
	}

	fmt.Fprintf(client, "Die")

}

func TestLargeLong(t *testing.T) {
	var c Config
	c.InsecureNoAuthenticateHandshake = true
	nc, err := Listen("127.0.0.1:3326", &c)
	if err != nil {
		t.Fatal(err)
	}
	defer close(nc)

	go func() {

		for c := range nc {

			go func(conn *Connection) {
				defer conn.Close()

				for {

					data := make([]byte, 2024)
					rand.Read(data)

					_, err := conn.Write(data)
					if err != nil {
						if err != io.EOF {
							t.Fatal(err)
						}
					}

					buf := make([]byte, 100)
					n, err := conn.Read(buf)
					if err != nil {
						t.Fatal(err)
					}

					if string(buf[:n]) != "More" {
						return
					}

				}

			}(c)
		}
	}()

	client, _, err := DialWithConfig("127.0.0.1:3326", &c)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	data := make([]byte, 948624)
	for x := 0; x < 1000; x++ {

		_, err := client.Read(data)
		if err != nil {
			t.Fatal(err)
		}

		fmt.Fprintf(client, "More")
	}

	fmt.Fprintf(client, "Die")

}

func BenchmarkLargeLong(b *testing.B) {
	var c Config
	c.InsecureNoAuthenticateHandshake = true

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer l.Close()

	newConnections := make(chan *Connection)

	go func() {
		defer close(newConnections)
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}

			s, _, err := Wrap(conn, &c)
			if err != nil {
				continue
			}
			newConnections <- s
		}
	}()

	go func() {

		for c := range newConnections {

			go func(conn *Connection) {
				defer conn.Close()

				for {

					data := make([]byte, 2024)
					rand.Read(data)

					_, err := conn.Write(data)
					if err != nil {
						if err != io.EOF {
							b.Fatal(err)
						}
					}

					buf := make([]byte, 100)
					n, err := conn.Read(buf)
					if err != nil {
						b.Fatal(err)
					}

					if string(buf[:n]) != "More" {
						return
					}

				}

			}(c)
		}
	}()

	client, _, err := DialWithConfig(l.Addr().String(), &c)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	data := make([]byte, 948624)
	for x := 0; x < b.N; x++ {

		_, err := client.Read(data)
		if err != nil {
			b.Fatal(err)
		}

		fmt.Fprintf(client, "More")
	}

	fmt.Fprintf(client, "Die")

}
