package tun

import (
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"

	"github.com/songgao/water"
)

// TUNDevice represents a TUN network interface
type TUNDevice struct {
	iface    *water.Interface
	name     string
	mtu      int
	localIP  net.IP
	remoteIP net.IP
	subnet   *net.IPNet

	// For route cleanup
	originalGateway string
	vpnServerIP     string
	
	// For DNS cleanup
	originalDNS     string
	networkService  string // e.g., "Wi-Fi" on macOS
}

// Config holds TUN device configuration
type Config struct {
	Name        string     // Interface name (optional, auto-generated if empty)
	MTU         int        // Maximum transmission unit
	LocalIP     net.IP     // Local IP address for the interface
	RemoteIP    net.IP     // Remote/server IP address
	Subnet      *net.IPNet // Subnet for the VPN network
	VPNServerIP string     // Real IP of VPN server (for route exclusion)
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
		iface:       iface,
		name:        iface.Name(),
		mtu:         cfg.MTU,
		localIP:     cfg.LocalIP,
		remoteIP:    cfg.RemoteIP,
		subnet:      cfg.Subnet,
		vpnServerIP: cfg.VPNServerIP,
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

// SetDefaultRoute redirects all traffic through the VPN
func (d *TUNDevice) SetDefaultRoute() error {
	switch runtime.GOOS {
	case "darwin":
		return d.setDefaultRouteDarwin()
	case "linux":
		return d.setDefaultRouteLinux()
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// setDefaultRouteDarwin sets default route on macOS
func (d *TUNDevice) setDefaultRouteDarwin() error {
	// Get current default gateway
	gateway, err := getDefaultGateway()
	if err != nil {
		return fmt.Errorf("failed to get default gateway: %w", err)
	}
	d.originalGateway = gateway
	fmt.Printf("Original gateway: %s\n", gateway)

	// Add route to VPN server via original gateway (so VPN traffic can reach server)
	if d.vpnServerIP != "" {
		cmd := exec.Command("route", "add", "-host", d.vpnServerIP, gateway)
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Printf("Warning: route to VPN server: %s\n", string(out))
		}
	}

	// Delete old default route
	cmd := exec.Command("route", "delete", "default")
	cmd.CombinedOutput() // Ignore error

	// Add new default route via VPN
	cmd = exec.Command("route", "add", "default", d.remoteIP.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add default route: %s: %w", string(out), err)
	}

	fmt.Printf("Default route set to VPN: %s\n", d.remoteIP.String())
	return nil
}

// setDefaultRouteLinux sets default route on Linux
func (d *TUNDevice) setDefaultRouteLinux() error {
	// Get current default gateway
	gateway, err := getDefaultGateway()
	if err != nil {
		return fmt.Errorf("failed to get default gateway: %w", err)
	}
	d.originalGateway = gateway

	// Add route to VPN server via original gateway
	if d.vpnServerIP != "" {
		cmd := exec.Command("ip", "route", "add", d.vpnServerIP, "via", gateway)
		cmd.CombinedOutput() // Ignore error if exists
	}

	// Add default route via VPN with lower metric
	cmd := exec.Command("ip", "route", "add", "default", "via", d.remoteIP.String(), "metric", "1")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add default route: %s: %w", string(out), err)
	}

	return nil
}

// RemoveDefaultRoute restores original routing
func (d *TUNDevice) RemoveDefaultRoute() error {
	switch runtime.GOOS {
	case "darwin":
		return d.removeDefaultRouteDarwin()
	case "linux":
		return d.removeDefaultRouteLinux()
	default:
		return nil
	}
}

// removeDefaultRouteDarwin restores routes on macOS
func (d *TUNDevice) removeDefaultRouteDarwin() error {
	// Delete VPN default route
	cmd := exec.Command("route", "delete", "default")
	cmd.CombinedOutput()

	// Restore original default route
	if d.originalGateway != "" {
		cmd = exec.Command("route", "add", "default", d.originalGateway)
		cmd.CombinedOutput()
	}

	// Remove route to VPN server
	if d.vpnServerIP != "" {
		cmd = exec.Command("route", "delete", "-host", d.vpnServerIP)
		cmd.CombinedOutput()
	}

	return nil
}

// removeDefaultRouteLinux restores routes on Linux
func (d *TUNDevice) removeDefaultRouteLinux() error {
	// Remove VPN default route
	cmd := exec.Command("ip", "route", "del", "default", "via", d.remoteIP.String())
	cmd.CombinedOutput()

	// Remove route to VPN server
	if d.vpnServerIP != "" {
		cmd = exec.Command("ip", "route", "del", d.vpnServerIP)
		cmd.CombinedOutput()
	}

	return nil
}

// getDefaultGateway gets the current default gateway
func getDefaultGateway() (string, error) {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("route", "-n", "get", "default")
	case "linux":
		cmd = exec.Command("ip", "route", "show", "default")
	default:
		return "", fmt.Errorf("unsupported OS")
	}

	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if runtime.GOOS == "darwin" && strings.HasPrefix(line, "gateway:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1], nil
			}
		}
		if runtime.GOOS == "linux" && strings.HasPrefix(line, "default") {
			parts := strings.Fields(line)
			for i, p := range parts {
				if p == "via" && i+1 < len(parts) {
					return parts[i+1], nil
				}
			}
		}
	}

	return "", fmt.Errorf("gateway not found")
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
	// Restore DNS before closing
	d.RestoreDNS()
	// Restore routes before closing
	d.RemoveDefaultRoute()
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

// SetDNS configures DNS to use public DNS servers
func (d *TUNDevice) SetDNS() error {
	switch runtime.GOOS {
	case "darwin":
		return d.setDNSDarwin()
	case "linux":
		return d.setDNSLinux()
	default:
		return nil
	}
}

// setDNSDarwin sets DNS on macOS
func (d *TUNDevice) setDNSDarwin() error {
	// Find the active network service
	service, err := getActiveNetworkService()
	if err != nil {
		return fmt.Errorf("failed to get network service: %w", err)
	}
	d.networkService = service

	// Get current DNS servers
	cmd := exec.Command("networksetup", "-getdnsservers", service)
	out, _ := cmd.Output()
	d.originalDNS = strings.TrimSpace(string(out))
	fmt.Printf("Original DNS: %s\n", d.originalDNS)

	// Set public DNS servers (Cloudflare + Google)
	cmd = exec.Command("networksetup", "-setdnsservers", service, "1.1.1.1", "8.8.8.8")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to set DNS: %s: %w", string(out), err)
	}

	fmt.Println("DNS set to: 1.1.1.1, 8.8.8.8")
	return nil
}

// setDNSLinux sets DNS on Linux
func (d *TUNDevice) setDNSLinux() error {
	// On Linux, modify /etc/resolv.conf or use resolvectl
	// For simplicity, we'll use resolvectl if available
	cmd := exec.Command("which", "resolvectl")
	if err := cmd.Run(); err == nil {
		// Use systemd-resolved
		cmd = exec.Command("resolvectl", "dns", d.name, "1.1.1.1", "8.8.8.8")
		cmd.CombinedOutput()
	}
	return nil
}

// RestoreDNS restores original DNS settings
func (d *TUNDevice) RestoreDNS() error {
	switch runtime.GOOS {
	case "darwin":
		return d.restoreDNSDarwin()
	case "linux":
		return d.restoreDNSLinux()
	default:
		return nil
	}
}

// restoreDNSDarwin restores DNS on macOS
func (d *TUNDevice) restoreDNSDarwin() error {
	if d.networkService == "" {
		return nil
	}

	var cmd *exec.Cmd
	if d.originalDNS == "" || strings.Contains(d.originalDNS, "There aren't any DNS Servers") {
		// Set to empty (use DHCP)
		cmd = exec.Command("networksetup", "-setdnsservers", d.networkService, "Empty")
	} else {
		// Restore original
		dnsServers := strings.Fields(d.originalDNS)
		args := append([]string{"-setdnsservers", d.networkService}, dnsServers...)
		cmd = exec.Command("networksetup", args...)
	}

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to restore DNS: %s: %w", string(out), err)
	}

	fmt.Println("DNS restored")
	return nil
}

// restoreDNSLinux restores DNS on Linux
func (d *TUNDevice) restoreDNSLinux() error {
	// systemd-resolved will automatically restore when interface goes down
	return nil
}

// getActiveNetworkService finds the active network service on macOS
func getActiveNetworkService() (string, error) {
	// Get the primary network service
	cmd := exec.Command("route", "-n", "get", "default")
	out, err := cmd.Output()
	if err != nil {
		return "Wi-Fi", nil // Default to Wi-Fi
	}

	// Find interface name
	var iface string
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "interface:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				iface = parts[1]
				break
			}
		}
	}

	if iface == "" {
		return "Wi-Fi", nil
	}

	// Map interface to service name
	cmd = exec.Command("networksetup", "-listallhardwareports")
	out, err = cmd.Output()
	if err != nil {
		return "Wi-Fi", nil
	}

	lines = strings.Split(string(out), "\n")
	var currentService string
	for _, line := range lines {
		if strings.HasPrefix(line, "Hardware Port:") {
			currentService = strings.TrimPrefix(line, "Hardware Port: ")
		}
		if strings.HasPrefix(line, "Device:") {
			device := strings.TrimSpace(strings.TrimPrefix(line, "Device:"))
			if device == iface {
				return currentService, nil
			}
		}
	}

	return "Wi-Fi", nil
}
