# simple-cast-server

A small, dependency-light bridge that lets Home Assistant start, stop and
monitor **casting a URL to a Chromecast / Google Nest Hub** over MQTT -
without needing [`catt`](https://github.com/skorokithakis/catt), Python, or
any Chromecast SDK.

## Why

Home Assistant's built-in Cast integration (and `catt`-based automations)
have a known issue where casting a website to a Google Nest Hub gets
silently aborted after roughly 10 minutes of being idle. `simple-cast-server`
works around this in two ways:

1. It talks to the device directly using a minimal, hand-rolled
   implementation of the Cast V2 protocol and the
   [DashCast](https://github.com/stestagg/dashcast) receiver app (app id
   `84912283`) - the same receiver app `catt cast_site` uses - so there's no
   Python/`catt` dependency in the container.
2. If the page you're casting is [immich-kiosk](https://github.com/damongolding/immich-kiosk)
   with its `cast_fix` option enabled, the page itself keeps the Cast
   session alive from the inside (periodically loading a dummy image via the
   Cast Application Framework), so `simple-cast-server` never needs to
   reload the page itself.

Home Assistant then controls casting through a single MQTT-discovered
`switch` entity per device - turning it on casts the device's configured
URL, turning it off stops casting, and the switch state reflects the real,
polled status of the device.

## Configuration

Devices are configured via a YAML file (default path
`/etc/simple-cast-server/devices.yaml`, override with `--config` or
`$CONFIG_FILE`):

```yaml
- id: nesthub-1
  target: https://immich-kiosk.example.com
  ip: 10.0.0.4

- id: nesthub-2
  target: https://immich-kiosk.example.com/?webhook_id=nesthub-2
  ip: 10.0.0.5
  port: 8009 # optional, defaults to 8009
```

| Field    | Required | Description                                             |
| -------- | -------- | --------------------------------------------------------|
| `id`     | yes      | Unique, stable identifier. Used to build MQTT topics.    |
| `target` | yes      | URL cast to the device via DashCast.                     |
| `ip`     | yes      | The device's IP address on the local network.            |
| `port`   | no       | Cast V2 control port. Defaults to `8009`.                 |

See [devices.example.yaml](devices.example.yaml).

### Command-line flags / environment variables

| Flag                 | Env var             | Default                                  | Description                                 |
| --------------------- | -------------------- | ----------------------------------------- | -------------------------------------------- |
| `--config`            | `CONFIG_FILE`        | `/etc/simple-cast-server/devices.yaml`    | Path to the devices YAML file.               |
| `--mqtt-broker`       | `MQTT_BROKER`        | `tcp://localhost:1883`                    | MQTT broker URL.                             |
| `--mqtt-username`     | `MQTT_USERNAME`      | *(empty)*                                 | MQTT username.                               |
| `--mqtt-password`     | `MQTT_PASSWORD`      | *(empty)*                                 | MQTT password.                               |
| `--mqtt-client-id`    | `MQTT_CLIENT_ID`     | `simple-cast-server`                      | MQTT client id.                              |
| `--topic-prefix`      | `TOPIC_PREFIX`       | `simple-cast-server`                      | Prefix for state/command topics.             |
| `--discovery-prefix`  | `DISCOVERY_PREFIX`   | `homeassistant`                           | Home Assistant MQTT discovery prefix.        |
| `--poll-interval`     | *(n/a)*              | `15s`                                     | How often each device's real status is polled. |
| `--log-level`         | `LOG_LEVEL`          | `info`                                    | `debug`, `info`, `warn` or `error`.           |

## MQTT topics

For a device with id `nesthub-1` and the default topic prefix:

| Topic                                                    | Direction | Payload            | Purpose                                   |
| --------------------------------------------------------- | --------- | ------------------ | ------------------------------------------ |
| `simple-cast-server/nesthub-1/set`                        | subscribe | `ON` / `OFF`        | Start / stop casting.                      |
| `simple-cast-server/nesthub-1/state`                      | publish   | `ON` / `OFF`        | Current (polled) casting state.            |
| `simple-cast-server/bridge/status`                        | publish   | `online`/`offline`  | Bridge availability (MQTT LWT).            |
| `homeassistant/switch/simple-cast-server_nesthub-1/config` | publish   | discovery JSON      | Home Assistant MQTT Discovery config.      |

Home Assistant will auto-create a `switch.nesthub_1` entity (or similar,
depending on your device naming) for every configured device the first time
the bridge connects - no manual `configuration.yaml` entries needed.

## Home Assistant usage

Once discovered, use the entity like any other switch:

```yaml
automation:
  - alias: Cast kiosk to Nest Hub every morning
    trigger:
      - platform: time
        at: "07:00:00"
    action:
      - service: switch.turn_on
        target:
          entity_id: switch.nesthub_1

  - alias: Stop casting at night
    trigger:
      - platform: time
        at: "22:00:00"
    action:
      - service: switch.turn_off
        target:
          entity_id: switch.nesthub_1
```

You can also check `switch.nesthub_1`'s state directly in the UI or other
automations/dashboards to see whether the device is currently casting.

## Running

### Docker / Podman

```sh
docker run -d \
  --name simple-cast-server \
  -v ./devices.yaml:/etc/simple-cast-server/devices.yaml:ro \
  -e MQTT_BROKER=tcp://mosquitto.local:1883 \
  ghcr.io/hellivan/simple-cast-server:latest
```

### From source

```sh
go run ./cmd/simple-cast-server --config devices.yaml --mqtt-broker tcp://localhost:1883
```

### Testing against a device directly (no MQTT needed)

[`cmd/cast-test`](cmd/cast-test) is a small standalone tool that exercises
the Chromecast/DashCast client directly, useful for verifying connectivity
to a device before wiring up MQTT:

```sh
go run ./cmd/cast-test --ip 10.0.4.2 --url https://immich-kiosk.example.com/
```

It starts casting the given URL, prints the device's status every 5
seconds, and stops casting cleanly on Ctrl+C.

## Building

```sh
make build   # go build ./...
make test    # go test ./...
make docker  # build the container image (REGISTRY ?= ghcr.io/hellivan)
```
