package tunnel

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// Endpoint represents a network endpoint with an optional username.
type Endpoint struct {
    Host string
    Port int
    User string
}

// NewEndpoint creates an endpoint from a string.
// It will extract username if provided, and the port if specified (defaulting to 0 if non is given.)
// This is like a constructor
// tstuser@127.0.0.1 -> will extract User: tstuser, Host: 127.0.0.1
// 222.2.2.222:22 -> will extract Host: 222.2.2.222, Port: 22
func NewEndpoint(s string) *Endpoint{

    endpoint := &Endpoint{
        Host: s,
    }

    if parts := strings.Split(endpoint.Host, "@"); len(parts) > 1 {
        endpoint.User = parts[0]
        endpoint.Host = parts[1]
    }  

    if parts := strings.Split(endpoint.Host, ":"); len(parts) > 1 {
        endpoint.Host = parts[0]
        endpoint.Port, _ = strconv.Atoi(parts[1])
    }

    return endpoint
}

// String returns the conventional formatted string host:port for an Endpoint.
func (endpoint Endpoint) String() string{

    return fmt.Sprintf("%s:%d", endpoint.Host, endpoint.Port)
}

// SSHTunnel encapsulates configuration and state for an SSH tunnel.
type SSHTunnel struct {

    Local  *Endpoint            // Local listening endpoint.
    Server *Endpoint            // SSH tunnel server (jump host).
    Remote *Endpoint            // Final destination endpoint.
    Config *ssh.ClientConfig    // SSH Client configuration.
    readyCh chan struct{}       // Signaling channel for when the tunnel is ready.
}

// NewSSHTunnel creates a new SSHTunnel instance.
// tunnelAddress is in the form "user@host[:port]".
// destination is the address (host:port) to connect from the server.
func NewSSHTunnel(tunnelAddress string, auth ssh.AuthMethod, destination string) *SSHTunnel{

    // Use port 0 to have the system choose a random free port.
    localEndpoint := NewEndpoint("localhost:0")

    // Default ssh to port 22
    server := NewEndpoint(tunnelAddress)
    if server.Port == 0 {
        server.Port = 22
    }

    return &SSHTunnel{
        Local: localEndpoint,
        Server: server,
        Remote: NewEndpoint(destination),
        Config: &ssh.ClientConfig{
            User: server.User,
            Auth: []ssh.AuthMethod{auth},

            // What is this?
            HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {

                // Always accept key.
                return nil
            },
            Timeout: 5 * time.Second, // Set a dial timeout for SSH connection.
        },
        readyCh: make(chan struct{}),
    }
}

// Start launches the SSH tunnel. It listens for incoming connections on the locally bound port,
// signals readiness via readyCh, and forwards connections. The method monitors the provided
// context and will shut down gracefully when the context is cancelled.
func (t *SSHTunnel) Start(ctx context.Context) error {

    listener, err := net.Listen("tcp", t.Local.String())
    if err != nil {
        return fmt.Errorf("failed to listen on %s. Here's why: %w", t.Local.String(), err)
    }

    // When the context is cancelled, close the listener so Accept() returns.
    go func(){
        <-ctx.Done()
        listener.Close()
    }()

    // Set the actual port assigned.
    t.Local.Port = listener.Addr().(*net.TCPAddr).Port

    // Signal that the tunnel is ready.
    close(t.readyCh)

    // Accept loop.
    for {
        conn, err := listener.Accept()
        if err != nil {
            select {

            case <- ctx.Done():

                // Expected error due to listener being closed on shutdown.
                return nil
            default:
                return fmt.Errorf("\nfailed to accept connection: %w", err)

            }
        }

        fmt.Printf("Connection accepted.\n")
        go t.forward(conn)
    }
}

func (t *SSHTunnel) WaitReady(ctx context.Context) error {
    select {
    case <-t.readyCh:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}


// forward handles a single connection: it establishes an SSH connection from the tunnel
// server to the remote endpoint and then sets up bidirectional copying.
func (tunnel *SSHTunnel) forward(localConn net.Conn) {

    defer localConn.Close()
    
    // Dial the SSH server
    serverConn, err := ssh.Dial("tcp", tunnel.Server.String(), tunnel.Config)
    if err != nil {
        log.Fatalf("\nError while dialing server. Here's why: %v", err)
    }
    defer serverConn.Close()

    fmt.Printf("Connected to %s [1 / 2]\n", tunnel.Server.String())

    // From the SSH server, dial the remote destination.
    remoteConn, err := serverConn.Dial("tcp", tunnel.Remote.String())
    if err != nil {
        log.Printf("\nError while dialing server. Here's why: %v", err)
        return
    }
    defer remoteConn.Close()

    fmt.Printf("Connected to %s [2 / 2]\n", tunnel.Remote.String())

    // Start bidirectional copy
    go func() {
        if _, err := io.Copy(localConn, remoteConn); err != nil {
            log.Printf("Error copying from remote to local: %v", err)
        }
    }()

    // Copy copies from src to dst until either EOF is reached
    // on src or an error occurs. It returns the number of bytes
    // copied and the first error encountered while copying, if any.
    if _, err := io.Copy(remoteConn, localConn); err != nil {
        log.Printf("Error copying from local to remote: %v", err)
    }

}

func Private_key_file(path string) ssh.AuthMethod {

    buffer, err := os.ReadFile(path)
    if err != nil {
        log.Printf("Error reading private key file (%s): %v", path, err)
        return nil
    }

    key, err := ssh.ParsePrivateKey(buffer)
    if err != nil {
        log.Printf("Error parsing private key from file (%s): %v", path, err)
        return nil
    }
    return ssh.PublicKeys(key)
}
