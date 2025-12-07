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

# Check IP forwarding (should be enabled on host)
if [ "$(cat /proc/sys/net/ipv4/ip_forward 2>/dev/null)" = "1" ]; then
    echo "✅ IP forwarding is enabled"
else
    echo "⚠️  IP forwarding is disabled!"
    echo "   Run on HOST: sudo sysctl -w net.ipv4.ip_forward=1"
    echo "   Continuing anyway..."
fi

# Create TUN device if not exists
echo "Setting up TUN device..."
mkdir -p /dev/net
if [ ! -c /dev/net/tun ]; then
    mknod /dev/net/tun c 10 200 2>/dev/null || true
fi
chmod 600 /dev/net/tun 2>/dev/null || true

# Setup NAT rules
echo "Configuring NAT..."

# Add masquerade for VPN subnet (ignore errors if rules exist)
iptables -t nat -C POSTROUTING -s 10.8.0.0/24 -o "$MAIN_IF" -j MASQUERADE 2>/dev/null || \
    iptables -t nat -A POSTROUTING -s 10.8.0.0/24 -o "$MAIN_IF" -j MASQUERADE

# Allow forwarding
iptables -C FORWARD -i hydra0 -o "$MAIN_IF" -j ACCEPT 2>/dev/null || \
    iptables -A FORWARD -i hydra0 -o "$MAIN_IF" -j ACCEPT 2>/dev/null || true

iptables -C FORWARD -i "$MAIN_IF" -o hydra0 -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || \
    iptables -A FORWARD -i "$MAIN_IF" -o hydra0 -m state --state RELATED,ESTABLISHED -j ACCEPT 2>/dev/null || true

echo ""
echo "✅ NAT configured for interface: $MAIN_IF"
echo "✅ VPN subnet: 10.8.0.0/24"
echo ""
echo "Starting HydraVPN server..."
echo ""

# Execute the main command
exec /app/hydra "$@"
