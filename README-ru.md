# lantern-headless-iran

[English](./README.md) · [فارسی](./README-fa.md) · **Русский**

> **Неофициальный** форк [`getlantern/lantern-headless-client`](https://github.com/getlantern/lantern-headless-client).
> Проект не связан с командой Lantern.

Клиент Lantern **без графического интерфейса** (headless), созданный для сетей с жёсткой
фильтрацией (проверено в Иране). Он открывает два локальных порта, к которым подключается ваш
браузер — трафик уходит через сеть Lantern.

## Два движка подключения

Команда `start` может работать на одном из двух движков; оба открывают одни и те же локальные
порты HTTP + SOCKS5, поэтому настройка браузера одинакова.

- **`--engine radiance` — официальный движок v10 (рекомендуется).** Запускает **тот же движок, что
  и официальный клиент Lantern v10** (Radiance + lantern-box). Он сам достигает серверов Lantern
  современным транспортом (domain fronting, прямой/умный дозвон, …), поэтому в большинстве
  фильтруемых сетей **подключается вообще без upstream-прокси** — именно этот режим реально обходит
  фильтр. Требует вспомогательный бинарник `lantern-radiance` рядом с CLI (публикуется в Releases).
- **`--engine flashlight` (по умолчанию) — исходный транспорт этого форка.** Легче, но в сетях,
  где IP серверов Lantern заблокированы, ему нужен рабочий `--upstream-proxy`. Сам по себе он
  может лишь получить конфиг через алиас Cloudflare и выдать подходящий IP выхода через этот
  upstream; фильтр он **не** обходит.

> В десктопном приложении [MischiefPingu](https://github.com/) движок radiance включён по
> умолчанию, и оба бинарника поставляются вместе, так что там флаги не нужны.

## Скачивание

Бинарные файлы **собираются автоматически средствами GitHub Actions** и публикуются на странице
[Releases](https://github.com/FossilizedProgrammer/lantern-headless-iran/releases).
Ничего не загружается с чьего-либо компьютера вручную. Каждый релиз публикует **один архив на
платформу**, в котором лежат **оба** бинарника:

| Файл | Платформа | Содержимое |
|---|---|---|
| `lantern-headless-linux-amd64.tar.gz` | Linux, x86_64 | `lantern` (CLI) + `lantern-radiance` (движок v10) |
| `lantern-headless-windows-amd64.zip` | Windows, 64 бит | `lantern.exe` (CLI) + `lantern-radiance.exe` (движок v10) |

Распакуйте архив — оба файла окажутся в одной папке, как раз там, где CLI их ищет. **Для
`--engine radiance` положите файл `lantern-radiance` (в Windows — `lantern-radiance.exe`) рядом с
CLI.** CLI ищет его рядом с собой (или по `--radiance-engine`, или в `$PATH`).
`--engine flashlight` в нём не нуждается.

Как это работает: каждый push в `main` собирает оба бинарника для каждой платформы (чтобы сборка
всегда была рабочей), а каждый тег версии `v*` дополнительно создаёт **Release** с этими архивами.

## Быстрый старт (radiance — рекомендуется)

1. Скачайте архив для своей системы со страницы **Releases** и распакуйте его (оба файла
   `lantern` и `lantern-radiance` окажутся рядом).
2. Запустите — **upstream-прокси не нужен**:

**Linux**

```shell
chmod +x lantern lantern-radiance
./lantern --data-path ./data start --engine radiance \
  --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

**Windows (Command Prompt)**

```bat
lantern.exe --data-path data start --engine radiance --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

3. Настройте браузер на локальные порты:

**Firefox** (рекомендуется — локальный DNS обычно отравлен):
`Настройки → Основные → Настройки сети → Ручная настройка прокси` → SOCKS Host `127.0.0.1`,
порт `18071`, тип **SOCKS v5**, включите **«Использовать DNS через SOCKS5»**.

**Chrome/Chromium**

```shell
google-chrome --proxy-server="socks5://127.0.0.1:18071"
```

> Клиент работает без интерфейса и никогда не меняет настройки системы сам. Если браузер не
> настроен, трафик пойдёт напрямую и не пройдёт через фильтр.

### Быстрый старт (flashlight — старый, нужен upstream)

Если вы предпочитаете прежний транспорт или у вас нет вспомогательного `lantern-radiance`,
запустите движок flashlight с рабочим локальным прокси (Tor, Psiphon, …) на порту `1080`:

**Linux**

```shell
./lantern --data-path ./data --upstream-proxy socks5://127.0.0.1:1080 \
  start --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

**Windows (Command Prompt)**

```bat
lantern.exe --data-path data --upstream-proxy socks5://127.0.0.1:1080 start --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

`--upstream-proxy` поддерживает схемы `socks5://`, `http://` и `https://`. В движке flashlight он
работает сразу в трёх ролях: получение конфига, соединение с серверами Lantern (их IP часто
заблокированы) и запасной канал для трафика. В `--engine radiance` он необязателен и служит только
для бутстрапа управляющего канала.

## Что означают сообщения при запуске

Движок radiance:

```
SUCCESS  Radiance engine connected to the Lantern network
INFO     Local proxy is listening — SOCKS5: 127.0.0.1:18071  HTTP: 127.0.0.1:18070
INFO     Verifying the data path with a real request through that proxy...
SUCCESS  Data path OK — HTTP 204 from https://www.google.com/generate_204 in 505ms
INFO     Traffic leaves this machine through IP 163.123.193.24
```

Движок flashlight:

```
SUCCESS  Control plane reachable (config fetched successfully)
INFO     Local proxy is listening — SOCKS5: 127.0.0.1:18071  HTTP: 127.0.0.1:18070
SUCCESS  Data path OK — HTTP 204 from https://www.google.com/generate_204 in 803ms
INFO     Traffic leaves this machine through IP 31.171.100.110
```

- **… connected / Control plane reachable** — подключение установлено; само по себе это ничего не
  говорит о трафике браузера.
- **Local proxy is listening** — порты открыты (открыт ≠ работающий).
- **Data path OK / NOT working** — самая важная строка: клиент выполнил реальный запрос через свой
  прокси. Если написано `NOT working`, следующая строка `Hint:` подскажет, что исправить.
- **Traffic leaves this machine through IP …** — тот публичный IP, который видит сайт назначения.

## Команды

| Команда | Что делает |
|---|---|
| `start [--engine radiance\|flashlight]` | Запускает локальные порты HTTP + SOCKS5 |
| `update [--via upstream\|direct\|auto] [--check] [--json]` | Обновляет сохранённый список серверов |
| `servers [--engine radiance\|flashlight] [--refresh] [--check] [--json]` | Список серверов со страной/городом; `--check` показывает доступность. Источник списка — выбранный движок |
| `start --server <name>` | Привязывает подключение к одному серверу (без автопереключения!) |
| `auth login / signup / logout` | Вход / регистрация в Lantern |
| `--help`, `--version` | Справка и версия |

Примеры (глобальные флаги ставятся **до** команды, флаги `start` — после неё):

```shell
./lantern --data-path ./data start --engine radiance
./lantern --data-path ./data update --via upstream
./lantern --data-path ./data servers --check
./lantern --data-path ./data servers --engine radiance --json
./lantern --data-path ./data start --engine radiance --server sunny-tokyo-02
```

## Основные параметры

| Параметр | Значение |
|---|---|
| `--engine <radiance\|flashlight>` | Движок подключения (по умолчанию `flashlight`); для паритета с v10 используйте `radiance` |
| `--radiance-engine <path>` | Путь к вспомогательному `lantern-radiance` (по умолчанию рядом с CLI, `LANTERN_RADIANCE_ENGINE` или `$PATH`) |
| `--upstream-proxy` | Ваш рабочий прокси: бутстрап управляющего канала; движку flashlight нужен для доступа к заблокированным серверам |
| `--data-path` | Где хранятся конфиг и логи (по умолчанию `~/.lantern-headless`) |
| `--server <name>` | Привязка `start` к одному серверу (см. `servers`) |
| `--with-proxyless` | Дополнительно пробовать прямой/умный дозвон для запросов управления (выключено по умолчанию) |
| `--debug` / `--quiet` / `--raw` | Уровень логирования |

Полный список: `--help`.

## Решение проблем

- **`could not find the lantern-radiance helper binary`** — распакуйте `lantern-radiance`
  (`lantern-radiance.exe`) из архива релиза и положите рядом с CLI, либо укажите
  `--radiance-engine /path/to/lantern-radiance`.
- **`--engine radiance` завершается до готовности** — запустите снова с `--debug`, чтобы увидеть
  собственные логи движка; подробности также пишутся в лог-файл в `--data-path`.
- **`update` не работает** — прямой доступ идёт через алиас Cloudflare (`df.getiantem.org`); если и
  он заблокирован, укажите `--upstream-proxy` с рабочим прокси. Ошибка `HTTP 401/403` означает:
  сначала выполните `auth login`.
- **`bind: address already in use`** — порт занят; укажите другие порты в
  `--http-proxy-addr` / `--socks-proxy-addr`.
- **SOCKS подключается, но страницы не открываются (движок flashlight)** — серверы Lantern
  недоступны из вашей сети; включите `--upstream-proxy` или перейдите на `--engine radiance`,
  который достигает их сам.
- **Сайт резолвится в `10.x.x.x`** — локальный DNS отравлен; клиент это замечает и разрешает имя
  хоста на стороне сервера (строка в логе: `keeping the hostname so it is resolved remotely`).

## Сборка из исходников

Самому CLI нужен [Go](https://go.dev/dl/) версии 1.24 или новее:

```shell
go build -o bin/lantern .
./bin/lantern --help
```

Движок radiance лежит в отдельном модуле (`radiance-engine/`) и **должен собираться с Go 1.26.x**
(Go 1.27 ломает один `//go:linkname` в sing-box). Соберите его целевой командой:

```shell
make radiance-engine   # результат: bin/lantern-radiance
```

В Linux он использует CGO; для кросс-сборки под Windows: `CGO_ENABLED=0 GOOS=windows GOARCH=amd64
make radiance-engine`. Положите получившийся бинарник рядом с CLI.

Чтобы собрать **всё** (CLI + движок radiance) для одной платформы одной командой, используйте скрипты:

```shell
./scripts/build-linux.sh     # dist/lantern-headless-linux-amd64.tar.gz   (lantern + lantern-radiance)
./scripts/build-windows.sh   # dist/lantern-headless-windows-amd64.zip    (lantern.exe + lantern-radiance.exe)
```

(скрипту для Windows нужен MinGW-w64 для самого CLI; его движок radiance — чистый Go). То же самое
доступно как `make build-local-linux` и `make build-local-windows`.

## Чем этот форк отличается от оригинала

- **Движок radiance (`--engine radiance`)**: полный паритет с официальным клиентом Lantern v10 —
  тот же data plane Radiance + lantern-box, собранный с тегом `novpn`, чтобы отдавать прокси
  HTTP/SOCKS вместо устройства TUN. Он сам подключается через современное обходное сопротивление,
  поэтому работает в фильтруемых сетях без upstream-прокси.
- **Алиас Cloudflare для управляющего канала**: запросы конфига/авторизации идут на
  `df.getiantem.org` вместо `df.iantem.io`, потому что периферия Google отдаёт `403` иранским IP —
  это тот же алиас, что использует собственный клиент Radiance для ограниченных регионов.
- **`--upstream-proxy` для движка flashlight**: открывает (часто заблокированное) TCP-соединение с
  серверами Lantern и служит запасным каналом — локальные порты отвечают, и трафик может уходить
  через сеть Lantern, даже когда ни один сервер напрямую недоступен.
- **Защита от отравленного DNS**: приватный IP от локального DNS никогда не набирается напрямую;
  имя хоста сохраняется и разрешается удалённо на сервере.
- **Команды `servers` и `update`**: список серверов с географией, проверка доступности и привязка
  `start` к одному серверу.
- **Честная диагностика**: перед тем как сообщить о работе, клиент делает реальный запрос через
  прокси.

## Лицензия

[Apache License 2.0](./LICENSE) — как и в оригинале. Lantern — товарный знак его владельцев; этот
проект является независительным неофициальным форком.
