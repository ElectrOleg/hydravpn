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

# Перезайдите в SSH для применения группы
exit
ssh user@YOUR_SERVER_IP
```

### 1.3 Запустите VPN сервер

```bash
cd ~/hydravpn

# Сборка и запуск (NAT настроится автоматически в контейнере!)
docker compose up -d

# Проверка логов
docker compose logs -f
```

> ✅ **Всё!** `setup-server.sh` запускать не нужно — контейнер сам настраивает NAT при старте.

---

## Шаг 2: Подключение с MacBook

```bash
cd /Users/olegavdeev/Desktop/My_Projects/VPN_tests

# Замените YOUR_SERVER_IP на реальный IP сервера
sudo ./hydra client --server YOUR_SERVER_IP:8443
```

---

## Проверка подключения

```bash
# Проверить TUN интерфейс
ifconfig | grep hydra

# Проверить внешний IP (должен показать IP сервера)
curl ifconfig.me
```

---

## Команды управления

```bash
# Статус
docker compose ps

# Логи
docker compose logs -f

# Остановка
docker compose down

# Перезапуск
docker compose restart
```
