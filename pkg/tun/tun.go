package tun

import (
	"fmt"
	"net"
	"os/exec"
	"runtime"

	"github.com/songgao/water"
)

// TUNDevice represents a TUN network interface
type TUNDevice struct {
	iface  *water.Interface
	name   string
	mtu    int
	localIP  net.IP
	remoteIP net.IP
	subnet   *net.IPNet
}

// Config holds TUN device configuration
type Config struct {
	Name     string // Interface name (optional, auto-generated if empty)
	MTU      int    // Maximum transmission unit
	LocalIP  net.IP // Local IP address for the interface
	RemoteIP net.IP // Remote/server IP address
	Subnet   *net.IPNet // Subnet for the VPN network
}

// DefaultConfig returns default TUN configuration
func DefaultConfig() *Config {
	_, subnet, _ := net.ParseCIDR("10.8.0.0/24")
	return &Config{
		Name:     "hydra0",
		MTU:      1400, // Leave room for VPN overhead
		LocalIP:  net.ParseIP("10.8.0.2"),
		RemoteIP: net.ParseIP("10.8.0.1"),
		Subnet:   subnet,
	}
}

// New creates and configures a new TUN device
func New(cfg *Config) (*TUNDevice, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Create TUN interface
	config := water.Config{
		DeviceType: water.TUN,
	}
	
	if cfg.Name != "" && runtime.GOOS != "darwin" {
		config.Name = cfg.Name
	}

	iface, err := water.New(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create TUN device: %w", err)
	}

	dev := &TUNDevice{
		iface:    iface,
		name:     iface.Name(),
		mtu:      cfg.MTU,
		localIP:  cfg.LocalIP,
		remoteIP: cfg.RemoteIP,
		subnet:   cfg.Subnet,
	}

	// Configure the interface
	if err := dev.configure(); err != nil {
		iface.Close()
		return nil, fmt.Errorf("failed to configure TUN device: %w", err)
	}

	return dev, nil
}

// configure sets up the TUN interface with IP and routes
func (d *TUNDevice) configure() error {
	switch runtime.GOOS {
	case "darwin":
		return d.configureDarwin()
	case "linux":
		return d.configureLinux()
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// configureDarwin configures TUN on macOS
func (d *TUNDevice) configureDarwin() error {
	// Set interface address
	cmd := exec.Command("ifconfig", d.name,
		d.localIP.String(), d.remoteIP.String(),
		"mtu", fmt.Sprintf("%d", d.mtu), "up")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ifconfig failed: %s: %w", string(out), err)
	}

	// Add route for VPN subnet
	if d.subnet != nil {
		ones, _ := d.subnet.Mask.Size()
		cmd = exec.Command("route", "add", "-net",
			d.subnet.IP.String()+"/"+fmt.Sprintf("%d", ones),
			d.remoteIP.String())
		if out, err := cmd.CombinedOutput(); err != nil {
			// Route might already exist, log but don't fail
			fmt.Printf("Route add warning: %s\n", string(out))
		}
	}

	return nil
}

// configureLinux configures TUN on Linux
func (d *TUNDevice) configureLinux() error {
	// Set interface up with IP
	cmd := exec.Command("ip", "link", "set", "dev", d.name, "mtu", fmt.Sprintf("%d", d.mtu), "up")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ip link set failed: %s: %w", string(out), err)
	}

	// Add IP address
	ones, _ := d.subnet.Mask.Size()
	cmd = exec.Command("ip", "addr", "add",
		fmt.Sprintf("%s/%d", d.localIP.String(), ones),
		"dev", d.name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ip addr add failed: %s: %w", string(out), err)
	}

	return nil
}

// Read reads a packet from the TUN device
func (d *TUNDevice) Read(b []byte) (int, error) {
	return d.iface.Read(b)
}

// Write writes a packet to the TUN device
func (d *TUNDevice) Write(b []byte) (int, error) {
	return d.iface.Write(b)
}

// Close closes the TUN device
func (d *TUNDevice) Close() error {
	return d.iface.Close()
}

// Name returns the interface name
func (d *TUNDevice) Name() string {
	return d.name
}

// MTU returns the MTU
func (d *TUNDevice) MTU() int {
	return d.mtu
}

// LocalIP returns the local IP
func (d *TUNDevice) LocalIP() net.IP {
	return d.localIP
}
