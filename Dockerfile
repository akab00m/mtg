###############################################################################
# BUILD STAGE

FROM golang:1.25.7-alpine AS build

RUN set -x \
  && apk --no-cache --update add \
    bash \
    ca-certificates \
    curl \
    git \
    make

COPY . /app
WORKDIR /app

RUN set -x \
  && make -j 4 static


###############################################################################
# PACKAGE STAGE

FROM scratch

# S1: Запуск от непривилегированного пользователя (nobody, uid=65534).
# scratch не содержит /etc/passwd — используем numeric UID.
# При RCE атакующий получает минимальные привилегии внутри контейнера.
USER 65534

ENTRYPOINT ["/mtg"]
CMD ["run", "/config.toml"]

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /app/mtg /mtg

# Q3: Не копируем example.config.toml — конфиг должен монтироваться через volume.
# Предыдущее поведение: example конфиг как default = риск запуска с небезопасными настройками.

# Q4: HEALTHCHECK через CLI-команду health.
# Проверяет Prometheus metrics endpoint (HTTP 200) или TCP connect к proxy порту.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/mtg", "health", "/config.toml"]
