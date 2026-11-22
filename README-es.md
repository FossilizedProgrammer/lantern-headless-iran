# lantern-headless-iran

[English](./README.md) · [فارسی](./README-fa.md) · [Русский](./README-ru.md) · [中文](./README-zh.md) · **Español**

> Fork **no oficial** de [`getlantern/lantern-headless-client`](https://github.com/getlantern/lantern-headless-client). Sin relación con el equipo de Lantern.

Un cliente de Lantern **sin interfaz gráfica** (headless), pensado para redes con censura
fuerte (probado en Irán). Abre dos puertos locales y tu navegador apunta a ellos: así el
tráfico sale a través de la red de Lantern.

## Dos motores de conexión

`start` puede ejecutarse con dos motores; ambos exponen los mismos puertos locales HTTP + SOCKS5,
por lo que la configuración del navegador es idéntica.

- **`--engine radiance`: el motor oficial v11 (recomendado).** Ejecuta **el mismo motor que el
  cliente oficial de Lantern v11** (Radiance + lantern-box, sing-box 1.14). Alcanza los servidores
  de Lantern por sí mismo con el transporte moderno (domain fronting, marcado directo/inteligente,
  …), así que en la mayoría de las redes censuradas **se conecta sin ningún proxy upstream**: este
  es el modo que realmente atraviesa el filtro. Necesita el binario auxiliar `lantern-radiance`
  junto al CLI (viene en las Releases).
- **`--engine flashlight` (predeterminado): el transporte original de este fork.** Más ligero,
  pero en redes que bloquean las IPs de los servidores de Lantern necesita un `--upstream-proxy`
  que funcione. Por sí solo únicamente puede obtener la configuración mediante el alias de
  Cloudflare y entregarte una IP de salida adecuada a través de ese upstream; **no** evita el
  filtro por sí mismo.

> En la aplicación de escritorio [MischiefPingu](https://github.com/) el motor radiance es el
> predeterminado y ambos binarios vienen incluidos, así que allí no hacen falta flags.

## Descarga

Los binarios se compilan **automáticamente con GitHub Actions** y se publican en la página de
[Releases](https://github.com/FossilizedProgrammer/lantern-headless-iran/releases). No se sube
nada desde la computadora de nadie. Cada versión publica **un archivo por plataforma que incluye
ambos binarios**:

| Archivo | Plataforma | Contenido |
|---|---|---|
| `lantern-headless-linux-amd64.tar.gz` | Linux, x86_64 | `lantern` (CLI) + `lantern-radiance` (motor v11) |
| `lantern-headless-windows-amd64.zip` | Windows, 64 bits | `lantern.exe` (CLI) + `lantern-radiance.exe` (motor v11) |

Extrae el archivo: ambos binarios quedan en la misma carpeta, que es exactamente donde el CLI
espera al auxiliar. **Para `--engine radiance` mantén `lantern-radiance` (`lantern-radiance.exe`
en Windows) junto al CLI.** El CLI lo busca a su lado (o en `--radiance-engine`, o en `$PATH`).
`--engine flashlight` no lo necesita.

Cómo funciona: cada push a `main` compila ambos binarios por plataforma (para que la compilación
siga verde) y cada etiqueta de versión `v*` crea además una **Release** con esos archivos.

## Inicio rápido (radiance, recomendado)

1. Descarga el archivo de tu sistema desde **Releases** y extráelo (tanto `lantern` como
   `lantern-radiance` acaban en la misma carpeta).
2. Ejecútalo: **no hace falta ningún proxy upstream**:

**Linux**

```shell
chmod +x lantern lantern-radiance
./lantern --data-path ./data start --engine radiance \
  --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

**Windows (símbolo del sistema)**

```bat
lantern.exe --data-path data start --engine radiance --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

3. Apunta el navegador a los puertos locales:

**Firefox** (recomendado: el DNS local suele estar envenenado):
`Ajustes → General → Configuración de red → Configuración manual del proxy` → Host SOCKS
`127.0.0.1`, puerto `18071`, tipo **SOCKS v5**, y marca **«Usar DNS a través de SOCKS5»**.

**Chrome/Chromium**

```shell
google-chrome --proxy-server="socks5://127.0.0.1:18071"
```

> El cliente es headless: nunca cambia la configuración del sistema por sí mismo. Si el navegador
> no está configurado, el tráfico sigue directo y no pasará el filtro.

### Inicio rápido (flashlight: heredado, necesita un upstream)

Si prefieres el transporte anterior, o falta el auxiliar de radiance, ejecuta el motor flashlight
con un proxy local que funcione (Tor, Psiphon, …) en el puerto `1080`:

**Linux**

```shell
./lantern --data-path ./data --upstream-proxy socks5://127.0.0.1:1080 \
  start --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

**Windows (símbolo del sistema)**

```bat
lantern.exe --data-path data --upstream-proxy socks5://127.0.0.1:1080 start --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

`--upstream-proxy` acepta `socks5://`, `http://` y `https://`. En el motor flashlight hace todo a
la vez: obtiene la configuración, abre la conexión (a menudo bloqueada por IP) a los servidores de
Lantern y actúa como ruta de datos de último recurso. Con `--engine radiance` es opcional y solo
sirve para arrancar el plano de control.

## Qué significan los mensajes de arranque

Motor radiance:

```
SUCCESS  Radiance engine connected to the Lantern network
INFO     Local proxy is listening — SOCKS5: 127.0.0.1:18071  HTTP: 127.0.0.1:18070
INFO     Verifying the data path with a real request through that proxy...
SUCCESS  Data path OK — HTTP 204 from https://www.google.com/generate_204 in 505ms
INFO     Traffic leaves this machine through IP 163.123.193.24
```

Motor flashlight:

```
SUCCESS  Control plane reachable (config fetched successfully)
INFO     Local proxy is listening — SOCKS5: 127.0.0.1:18071  HTTP: 127.0.0.1:18070
SUCCESS  Data path OK — HTTP 204 from https://www.google.com/generate_204 in 803ms
INFO     Traffic leaves this machine through IP 31.171.100.110
```

- **… connected / Control plane reachable**: la conexión está establecida. Por sí solo no dice
  nada sobre el tráfico del navegador.
- **Local proxy is listening**: los puertos están abiertos (abierto ≠ funcionando).
- **Data path OK / NOT working**: la línea que importa: el cliente hizo una petición real a través
  de su propio proxy. Si falla, la línea `Hint:` justo debajo dice qué corregir.
- **Traffic leaves this machine through IP …**: la IP pública que ve el destino.

## Comandos

| Comando | Qué hace |
|---|---|
| `start [--engine radiance\|flashlight]` | Arranca el proxy local HTTP + SOCKS5 |
| `update [--via upstream\|direct\|auto] [--check] [--json]` | Actualiza la lista de servidores guardada |
| `servers [--engine radiance\|flashlight] [--refresh] [--check] [--json]` | Lista los servidores con país/ciudad. La lista viene del motor elegido; `--check` prueba la accesibilidad **a través de ese motor** (Radiance ejecuta sus propias pruebas de URL por servidor; el `--check` de flashlight sondea directamente). |
| `start --server <name>` | Fija la conexión a un único servidor (sin conmutación automática) |
| `auth login / signup / logout` | Iniciar sesión / crear cuenta de Lantern |
| `--help`, `--version` | Ayuda y versión |

Ejemplos (los flags globales van **antes** del comando; los de `start`, después):

```shell
./lantern --data-path ./data start --engine radiance
./lantern --data-path ./data update --via upstream
./lantern --data-path ./data servers --check
./lantern --data-path ./data servers --engine radiance --json
./lantern --data-path ./data start --engine radiance --server sunny-tokyo-02
```

## Opciones principales

| Opción | Significado |
|---|---|
| `--engine <radiance\|flashlight>` | Motor de conexión (por defecto `flashlight`). Usa `radiance` para la paridad con v11 |
| `--radiance-engine <path>` | Ruta al auxiliar `lantern-radiance` (por defecto: junto al CLI, `LANTERN_RADIANCE_ENGINE` o `$PATH`) |
| `--upstream-proxy` | Tu proxy que funciona: arranque del plano de control; el motor flashlight lo necesita para alcanzar servidores bloqueados |
| `--data-path` | Dónde viven la configuración y los logs (por defecto `~/.lantern-headless`) |
| `--server <name>` | Fija `start` a un servidor por nombre (ver `servers`) |
| `--with-proxyless` | También probar marcado directo/inteligente para las peticiones de control (desactivado por defecto) |
| `--debug` / `--quiet` / `--raw` | Registro |

Lista completa: `--help`.

## Solución de problemas

- **`could not find the lantern-radiance helper binary`**: extrae `lantern-radiance`
  (`lantern-radiance.exe`) del archivo de la release y colócalo junto al CLI, o pasa
  `--radiance-engine /path/to/lantern-radiance`.
- **`--engine radiance` termina antes de estar listo**: vuelve a ejecutarlo con `--debug` para ver
  los logs del propio motor; los detalles también se escriben en el archivo de log de `--data-path`.
- **Falla `update`**: el acceso directo usa el alias de Cloudflare (`df.getiantem.org`); si también
  está bloqueado, pon `--upstream-proxy` con un proxy que funcione. `HTTP 401/403` significa que
  primero debes ejecutar `auth login`.
- **`bind: address already in use`**: el puerto está ocupado; da otros puertos a
  `--http-proxy-addr` / `--socks-proxy-addr`.
- **SOCKS conecta pero las páginas no abren (motor flashlight)**: los servidores de Lantern no son
  accesibles desde tu red; activa `--upstream-proxy` o cambia a `--engine radiance`, que los
  alcanza por sí mismo.
- **Un sitio resuelve a `10.x.x.x`**: el DNS local está envenenado; el cliente lo detecta y resuelve
  el nombre de host de forma remota (línea de log: `keeping the hostname so it is resolved remotely`).

## Compilar desde el código fuente

El CLI necesita [Go](https://go.dev/dl/) 1.24 o superior:

```shell
go build -o bin/lantern .
./bin/lantern --help
```

El motor radiance vive en su propio módulo (`radiance-engine/`) y **debe compilarse con Go 1.26.x**
(Go 1.27 rompe un `//go:linkname` de sing-box). Compílalo con el objetivo auxiliar:

```shell
make radiance-engine   # escribe bin/lantern-radiance
```

En Linux usa CGO; para una compilación cruzada a Windows usa `CGO_ENABLED=0 GOOS=windows GOARCH=amd64
make radiance-engine`. Coloca el auxiliar resultante junto al CLI.

Para compilar **todo** (CLI + auxiliar de radiance) para una plataforma en un solo paso, usa los
scripts:

```shell
./scripts/build-linux.sh     # dist/lantern-headless-linux-amd64.tar.gz   (lantern + lantern-radiance)
./scripts/build-windows.sh   # dist/lantern-headless-windows-amd64.zip    (lantern.exe + lantern-radiance.exe)
```

(el script de Windows necesita MinGW-w64 para el CLI; su auxiliar de radiance es Go puro). Los
mismos objetivos están disponibles como `make build-local-linux` / `make build-local-windows`.

## Qué cambia este fork

- **Motor Radiance (`--engine radiance`)**: paridad completa con el cliente oficial de Lantern
  v11: el mismo plano de datos Radiance + lantern-box (sing-box 1.14.2, como en v11), compilado con
  la etiqueta `novpn` para servir un proxy HTTP/SOCKS en lugar de un dispositivo TUN. Se conecta por
  sí mismo mediante resistencia a la censura moderna, así que funciona en redes censuradas sin proxy
  upstream.
- **Latencia por servidor** (paridad con v10.0.4/v11): `servers --check` informa cuánto tardó en
  responder cada servidor, marca el más rápido, y `--json` la entrega como `latency_ms`.
- **Avisos de servidor fijado** (paridad con v11): fijar con `--server` desactiva la conmutación
  automática, así que el servidor fijado se sondea una vez y se imprime un aviso cuando no responde.
- **Alias de Cloudflare para el plano de control**: las peticiones de configuración/autorización van
  a `df.getiantem.org` en lugar de `df.iantem.io`, porque el borde de Google devuelve `403` a las
  IPs iraníes; es el mismo alias que el propio cliente Radiance de Lantern usa en regiones
  restringidas.
- **`--upstream-proxy` para el motor flashlight**: abre la conexión TCP (a menudo bloqueada) a los
  servidores de Lantern y actúa como ruta de datos de último recurso, de modo que los puertos
  locales siguen respondiendo y el tráfico puede salir por la red de Lantern incluso cuando ningún
  servidor es accesible directamente.
- **Manejo de DNS envenenado**: una IP privada devuelta por el DNS local nunca se marca; el nombre
  de host se conserva y el servidor lo resuelve de forma remota.
- **Comandos `servers` y `update`**: lista de servidores con ubicaciones, comprobaciones de
  accesibilidad y fijación de `start` a un servidor.
- **Diagnóstico de arranque honesto**: se hace una petición real a través del proxy antes de
  afirmar que algo funciona.

## Licencia

[Apache License 2.0](./LICENSE), igual que el proyecto original. Lantern es una marca de sus
propietarios; este proyecto es un fork independiente y no oficial.
