package antivirus

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"time"
)

// Status representa el resultado de un escaneo antivirus (PP-F3-ARQ-014, PP-F3-SEC-005).
type Status string

const (
	StatusPending  Status = "pending"
	StatusClean    Status = "clean"
	StatusInfected Status = "infected"
)

// EICARStandardSignature es la cadena estándar de prueba antivirus EICAR.
const EICARStandardSignature = `X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`

// Result es el resultado devuelto por un Scanner.
type Result struct {
	Status    Status
	VirusName string
}

// Scanner define la interfaz para el análisis de virus en archivos adjuntos.
type Scanner interface {
	Scan(ctx context.Context, r io.Reader) (Result, error)
}

// InProcessScanner es un escáner en memoria para CI, pruebas y entornos sin clamd.
// Detecta la firma EICAR y cadenas simuladas de prueba.
type InProcessScanner struct{}

func NewInProcessScanner() *InProcessScanner {
	return &InProcessScanner{}
}

func (s *InProcessScanner) Scan(ctx context.Context, r io.Reader) (Result, error) {
	data, err := io.ReadAll(io.LimitReader(r, 10<<20)) // 10 MB límite de escaneo
	if err != nil {
		return Result{Status: StatusPending}, err
	}

	content := string(data)
	if strings.Contains(content, EICARStandardSignature) ||
		strings.Contains(content, "EICAR-STANDARD-ANTIVIRUS-TEST-FILE") ||
		strings.Contains(content, "MALWARE-TEST-SIGNATURE") {
		return Result{
			Status:    StatusInfected,
			VirusName: "Eicar-Test-Signature",
		}, nil
	}

	return Result{Status: StatusClean}, nil
}

// ClamAVScanner implementa Scanner comunicándose con el daemon clamd por TCP o socket Unix
// usando el comando zINSTREAM del protocolo de ClamAV (PP-F3-ARQ-014).
type ClamAVScanner struct {
	network string
	address string
	timeout time.Duration
}

func NewClamAVScanner(network, address string, timeout time.Duration) *ClamAVScanner {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &ClamAVScanner{
		network: network,
		address: address,
		timeout: timeout,
	}
}

func (c *ClamAVScanner) Scan(ctx context.Context, r io.Reader) (Result, error) {
	dialer := &net.Dialer{Timeout: c.timeout}
	conn, err := dialer.DialContext(ctx, c.network, c.address)
	if err != nil {
		return Result{Status: StatusPending}, fmt.Errorf("clamav dial %s://%s: %w", c.network, c.address, err)
	}
	defer func() { _ = conn.Close() }()

	_ = conn.SetDeadline(time.Now().Add(c.timeout))

	// Comando zINSTREAM\0
	if _, err := conn.Write([]byte("zINSTREAM\x00")); err != nil {
		return Result{Status: StatusPending}, fmt.Errorf("clamav write command: %w", err)
	}

	buf := make([]byte, 32*1024)
	for {
		select {
		case <-ctx.Done():
			return Result{Status: StatusPending}, ctx.Err()
		default:
		}

		n, rerr := r.Read(buf)
		if n > 0 {
			if n > math.MaxUint32 {
				return Result{Status: StatusPending}, errors.New("invalid chunk size")
			}
			var lenBuf [4]byte
			binary.BigEndian.PutUint32(lenBuf[:], uint32(n))
			if _, err := conn.Write(lenBuf[:]); err != nil {
				return Result{Status: StatusPending}, fmt.Errorf("clamav write chunk len: %w", err)
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return Result{Status: StatusPending}, fmt.Errorf("clamav write chunk data: %w", err)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return Result{Status: StatusPending}, fmt.Errorf("read stream: %w", rerr)
		}
	}

	// Chunk terminador (longitud 0)
	var termBuf [4]byte
	if _, err := conn.Write(termBuf[:]); err != nil {
		return Result{Status: StatusPending}, fmt.Errorf("clamav write terminator: %w", err)
	}

	// Leer respuesta
	var respBuf bytes.Buffer
	reply := make([]byte, 1024)
	for {
		n, err := conn.Read(reply)
		if n > 0 {
			respBuf.Write(reply[:n])
			if bytes.Contains(respBuf.Bytes(), []byte{0}) || bytes.Contains(respBuf.Bytes(), []byte{'\n'}) {
				break
			}
		}
		if err != nil {
			break
		}
	}

	resp := strings.TrimSpace(respBuf.String())
	// Respuestas habituales:
	// "stream: OK"
	// "stream: <virus> FOUND"
	if strings.HasSuffix(resp, "OK") {
		return Result{Status: StatusClean}, nil
	}
	if strings.Contains(resp, "FOUND") {
		parts := strings.Split(resp, ":")
		virus := "Malware"
		if len(parts) >= 2 {
			virus = strings.TrimSpace(strings.TrimSuffix(parts[1], "FOUND"))
		}
		return Result{Status: StatusInfected, VirusName: virus}, nil
	}

	return Result{Status: StatusPending}, fmt.Errorf("clamav unexpected response: %s", resp)
}
