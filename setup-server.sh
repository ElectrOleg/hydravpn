#!/bin/bash
# HydraVPN Server Setup Script for Ubuntu
# Run with: sudo bash setup-server.sh

set -e

echo "╦ ╦╦ ╦╔╦╗╦═╗╔═╗  ╦  ╦╔═╗╔╗╔"
echo "╠═╣╚╦╝ ║║╠╦╝╠═╣  ╚╗╔╝╠═╝║║║"
echo "╩ ╩ ╩ ═╩╝╩╚═╩ ╩   ╚╝ ╩  ╝╚╝"
echo "  Server Setup Script"
echo ""

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "Please run as root: sudo bash setup-server.sh"
    exit 1
fi

# Get main network interface
MAIN_IF=$(ip route | grep default | awk '{print $5}' | head -1)
echo "Detected main interface: $MAIN_IF"

# 1. Enable IP forwarding
echo "Enabling IP forwarding..."
echo 'net.ipv4.ip_forward=1' > /etc/sysctl.d/99-hydravpn.conf
sysctl -p /etc/sysctl.d/99-hydravpn.conf

# 2. Create TUN device if not exists
echo "Creating TUN device..."
mkdir -p /dev/net
if [ ! -c /dev/net/tun ]; then
    mknod /dev/net/tun c 10 200
fi
chmod 600 /dev/net/tun

# 3. Setup iptables NAT rules
echo "Configuring NAT rules..."

# Clear existing rules for our chain
iptables -t nat -D POSTROUTING -s 10.8.0.0/24 -o $MAIN_IF -j MASQUERADE 2>/dev/null || true
iptables -D FORWARD -i hydra0 -o $MAIN_IF -j ACCEPT 2>/dev/null || true
iptables -D FORWARD -i $MAIN_IF -o hydra0 -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || true

# Add NAT rules
iptables -t nat -A POSTROUTING -s 10.8.0.0/24 -o $MAIN_IF -j MASQUERADE
iptables -A FORWARD -i hydra0 -o $MAIN_IF -j ACCEPT
iptables -A FORWARD -i $MAIN_IF -o hydra0 -m state --state RELATED,ESTABLISHED -j ACCEPT

# 4. Save iptables rules
echo "Saving iptables rules..."
if command -v netfilter-persistent &> /dev/null; then
    netfilter-persistent save
elif command -v iptables-save &> /dev/null; then
    iptables-save > /etc/iptables.rules
    echo '#!/bin/sh' > /etc/network/if-pre-up.d/iptables
    echo 'iptables-restore < /etc/iptables.rules' >> /etc/network/if-pre-up.d/iptables
    chmod +x /etc/network/if-pre-up.d/iptables
fi

# 5. Open firewall port
echo "Opening firewall port 8443..."
if command -v ufw &> /dev/null; then
    ufw allow 8443/tcp
    ufw allow 8443/udp
fi

echo ""
echo "✅ Server setup complete!"
echo ""
echo "Next steps:"
echo "  1. Build and run: docker-compose up -d"
echo "  2. Check logs: docker-compose logs -f"
echo ""
