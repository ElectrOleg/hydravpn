# HydraVPN Deployment Guide

## Шаг 1: Подготовка сервера (Ubuntu)

### 1.1 Скопируйте проект на сервер

```bash
# На вашем MacBook
scp -r /Users/olegavdeev/Desktop/My_Projects/VPN_tests user@YOUR_SERVER_IP:~/hydravpn
```

### 1.2 Установите Docker на сервере

```bash
# SSH на сервер
ssh user@YOUR_SERVER_IP

# Установите Docker
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker $USER

# Установите Docker Compose
sudo apt install docker-compose-plugin -y
```

### 1.3 Запустите setup скрипт

```bash
cd ~/hydravpn
sudo bash setup-server.sh
```

### 1.4 Запустите VPN сервер

```bash
# Сборка и запуск
docker compose up -d

# Проверка логов
docker compose logs -f
```

---

## Шаг 2: Подключение с MacBook

### 2.1 Соберите клиент (если еще не собран)

```bash
cd /Users/olegavdeev/Desktop/My_Projects/VPN_tests
go build -o hydra ./cmd/hydra
```

### 2.2 Подключитесь к серверу

```bash
# Замените YOUR_SERVER_IP на реальный IP вашего сервера
sudo ./hydra client --server YOUR_SERVER_IP:8443
```

---

## Проверка подключения

После успешного подключения:

```bash
# Проверьте TUN интерфейс
ifconfig | grep hydra

# Проверьте ваш внешний IP (должен показать IP сервера)
curl ifconfig.me
```

---

## Решение проблем

### Ошибка подключения
```bash
# На сервере проверьте, что порт открыт
sudo netstat -tlnp | grep 8443

# Проверьте firewall
sudo ufw status
```

### TUN не создается
```bash
# На сервере
sudo modprobe tun
ls -la /dev/net/tun
```

### NAT не работает
```bash
# На сервере
sudo iptables -t nat -L -n -v
sudo sysctl net.ipv4.ip_forward
```

---

## Остановка сервера

```bash
docker compose down
```
