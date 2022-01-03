package noodle

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/NHAS/chacha20blake2s"
	"golang.org/x/crypto/blake2s"
	"golang.org/x/crypto/curve25519"
)

type Connection struct {
	conn net.Conn

	sessKey            []byte
	rCounter, wCounter uint64
	staticPriv         ed25519.PrivateKey
	staticPublic       crypto.PublicKey

	sync.Mutex
	readBuffer []byte

	enc *chacha20blake2s.Chacha20blake2s
}

type Config struct {
	TrustStore                      []ed25519.PublicKey
	InsecureNoAuthenticateHandshake bool
	Timeout                         time.Duration
	PrivateKey                      ed25519.PrivateKey
}

func (s *Connection) handshake(conf *Config) error {
	ephemeralPrivate := make([]byte, curve25519.ScalarSize)
	if _, err := rand.Read(ephemeralPrivate); err != nil {
		return err
	}

	ephPub, err := curve25519.X25519(ephemeralPrivate, curve25519.Basepoint)
	if err != nil {
		return err
	}

	message := make([]byte, 8+len(ephPub), 8+len(ephPub)+ed25519.SignatureSize)
	validUntil := uint64(time.Now().Unix()) + 10
	binary.BigEndian.PutUint64(message, validUntil)
	copy(message[8:], ephPub)

	sig := ed25519.Sign(s.staticPriv, message)

	signaturePos := len(message)
	message = message[:cap(message)]

	copy(message[signaturePos:], sig)

	errs := make(chan error)
	go func() {
		_, err = s.conn.Write(message)
		errs <- err
	}()

	response := make([]byte, 8+len(ephPub)+ed25519.SignatureSize)

	_, err = io.ReadFull(s.conn, response)
	if err != nil {
		return err
	}

	if !conf.InsecureNoAuthenticateHandshake {
		exSig := response[len(response)-ed25519.SignatureSize:]

		found := false
		for _, key := range conf.TrustStore {
			if ed25519.Verify(key, response[:len(response)-ed25519.SignatureSize], exSig) {
				found = true
				break
			}

		}

		if !found {
			return errors.New("Key was not trusted")
		}
	}

	exHandshakeValid := binary.BigEndian.Uint64(response[:8])

	if exHandshakeValid > uint64(time.Now().Unix())+10 || uint64(time.Now().Unix()) > exHandshakeValid {
		return errors.New("The handshake was too far in the past, this is either a super slow connection, or a machine is badly out of time")
	}

	exEphPub := response[8 : 8+len(ephPub)]

	shared, err := curve25519.X25519(ephemeralPrivate, exEphPub)
	if err != nil {
		return err
	}

	key := blake2s.Sum256(shared)

	s.sessKey = key[:]

	s.enc, err = chacha20blake2s.New(s.sessKey)
	if err != nil {
		return err
	}

	return <-errs
}

func Wrap(conn net.Conn, c *Config) (s *Connection, private_s ed25519.PrivateKey, err error) {
	s = &Connection{}

	if !c.InsecureNoAuthenticateHandshake {
		if len(c.TrustStore) == 0 {
			return nil, nil, errors.New("No trusted public keys specified, but not marked as insecure")
		}
	}

	if c.PrivateKey != nil {
		s.staticPriv = c.PrivateKey
		s.staticPublic = c.PrivateKey.Public()
	}

	if c.PrivateKey == nil {

		s.staticPublic, s.staticPriv, err = ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, nil, err
		}
	}

	s.conn = conn

	err = s.handshake(c)
	if err != nil {
		return nil, nil, err
	}

	return s, s.staticPriv, nil
}

func InsecureDial(addr string) (s *Connection, private_s ed25519.PrivateKey, err error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	var c Config
	c.InsecureNoAuthenticateHandshake = true

	return Wrap(conn, &c)
}

func DialWithConfig(addr string, config *Config) (s *Connection, private_s ed25519.PrivateKey, err error) {

	d := net.Dialer{
		Timeout: config.Timeout,
	}

	conn, err := d.Dial("tcp", addr)
	if err != nil {
		return nil, nil, err
	}

	return Wrap(conn, config)

}

func Listen(addr string, config *Config) (newConnections chan *Connection, err error) {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	newConnections = make(chan *Connection)

	go func() {

		defer close(newConnections)
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}

			s, _, err := Wrap(conn, config)
			if err != nil {
				continue
			}
			newConnections <- s
		}
	}()

	return
}

func (s *Connection) Read(b []byte) (n int, err error) {

	s.Lock()
	if len(s.readBuffer) > 0 {
		n := copy(b, s.readBuffer)

		s.readBuffer = s.readBuffer[n:]
		s.Unlock()
		return n, nil
	}
	s.Unlock()

	sizeBuf := make([]byte, 2)
	_, err = io.ReadFull(s.conn, sizeBuf)
	if err != nil {
		return 0, err
	}

	size := binary.BigEndian.Uint16(sizeBuf)

	end := 0
	fullMessage := make([]byte, size)

	for {

		n, err = s.conn.Read(fullMessage[end:])
		if err != nil {
			return 0, err
		}

		end = n

		if len(fullMessage) == int(size) {
			break
		}
	}

	plaintext, err := s.enc.Open(fullMessage)
	if err != nil {
		return 0, err
	}

	counter := binary.BigEndian.Uint64(plaintext[:8])
	if counter != s.rCounter {
		return 0, errors.New("Replayed packet detected")
	}

	s.rCounter++

	plaintext = plaintext[8:]

	n = copy(b, plaintext)
	if n < len(plaintext) {
		s.Lock()
		s.readBuffer = append(s.readBuffer, plaintext[n:]...)
		s.Unlock()
	}

	return n, nil
}

func (s *Connection) Write(b []byte) (n int, err error) {

	if len(b) > 65535-s.enc.Overhead()-8 {
		return 0, fmt.Errorf("payload too big to send %d max %d", len(b), 65535-s.enc.Overhead()-8)
	}

	cnt := make([]byte, 8, 8+len(b))
	binary.BigEndian.PutUint64(cnt, s.wCounter)

	cnt = append(cnt, b...)

	ciphertext, err := s.enc.Seal(cnt)
	if err != nil {
		return 0, err
	}
	s.wCounter++

	finalMessage := make([]byte, 2, len(ciphertext)+2)
	binary.BigEndian.PutUint16(finalMessage, uint16(len(ciphertext)))

	finalMessage = append(finalMessage, ciphertext...)

	return s.conn.Write(finalMessage)
}

func (s *Connection) Close() error {

	return s.conn.Close()
}
