#!/bin/sh
set -e

echo "╦ ╦╦ ╦╔╦╗╦═╗╔═╗  ╦  ╦╔═╗╔╗╔"
echo "╠═╣╚╦╝ ║║╠╦╝╠═╣  ╚╗╔╝╠═╝║║║"
echo "╩ ╩ ╩ ═╩╝╩╚═╩ ╩   ╚╝ ╩  ╝╚╝"
echo "  Starting VPN Server..."
echo ""

# Detect main interface
MAIN_IF=$(ip route | grep default | awk '{print $5}' | head -1)
echo "Main interface: $MAIN_IF"

# Enable IP forwarding
echo "Enabling IP forwarding..."
echo 1 > /proc/sys/net/ipv4/ip_forward

# Create TUN device if not exists
echo "Setting up TUN device..."
mkdir -p /dev/net
if [ ! -c /dev/net/tun ]; then
    mknod /dev/net/tun c 10 200
fi
chmod 600 /dev/net/tun

# Setup NAT rules
echo "Configuring NAT..."

# Flush existing NAT rules for our subnet
iptables -t nat -F POSTROUTING 2>/dev/null || true

# Add masquerade for VPN subnet
iptables -t nat -A POSTROUTING -s 10.8.0.0/24 -o "$MAIN_IF" -j MASQUERADE

# Allow forwarding
iptables -A FORWARD -i hydra0 -o "$MAIN_IF" -j ACCEPT 2>/dev/null || true
iptables -A FORWARD -i "$MAIN_IF" -o hydra0 -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || true

echo ""
echo "✅ NAT configured for interface: $MAIN_IF"
echo "✅ VPN subnet: 10.8.0.0/24"
echo ""
echo "Starting HydraVPN server..."
echo ""

# Execute the main command
exec /app/hydra "$@"
