# lantern-headless-iran

[English](./README.md) · **فارسی** · [Русский](./README-ru.md)

> فورک **غیررسمی** از [`getlantern/lantern-headless-client`](https://github.com/getlantern/lantern-headless-client).
> هیچ وابستگی به تیم لنترن ندارد.

کلاینت لنترن **بدون رابط گرافیکی** (headless)، ساخته‌شده برای شبکه‌های با فیلترینگ شدید
(تست‌شده در ایران). کلاینت دو پورت محلی باز می‌کند و مرورگر شما به آن‌ها وصل می‌شود؛ ترافیک از
شبکهٔ لنترن بیرون می‌رود.

## دو موتور اتصال

دستور `start` می‌تواند با یکی از دو موتور اجرا شود؛ هر دو همان پورت‌های محلی HTTP + SOCKS5 را باز
می‌کنند، پس تنظیم مرورگر یکی است.

- **`--engine radiance` — موتور رسمی نسخهٔ ۱۰ (پیشنهادی).** همان **موتور رسمی کلاینت لنترن نسخهٔ
  ۱۰** را اجرا می‌کند (Radiance + lantern-box). خودش با ترنسپورت مدرن (domain fronting،
  dialing مستقیم/هوشمند و…) به سرورهای لنترن می‌رسد، پس در بیشتر شبکه‌های فیلترشده **بدون هیچ
  آپ‌استریمی وصل می‌شود** — این همان حالتی است که واقعاً از فیلتر عبور می‌کند. به باینری کمکی
  `lantern-radiance` کنار CLI نیاز دارد (در Releases منتشر می‌شود).
- **`--engine flashlight` (پیش‌فرض) — ترنسپورت اصلی همین فورک.** سبک‌تر است، اما در شبکه‌ای که
  IP سرورهای لنترن مسدود است به یک `--upstream-proxy` سالم نیاز دارد. به‌تنهایی فقط می‌تواند
  کانفیگ را از آلیاس Cloudflare بگیرد و از مسیر همان آپ‌استریم یک IP خروجی مناسب بدهد؛ خودش از
  فیلتر عبور **نمی‌کند**.

> در اپ دسکتاپ [MischiefPingu](https://github.com/) موتور radiance پیش‌فرض است و هر دو باینری
> همراه برنامه هستند، پس آنجا به هیچ فلگی نیاز نیست.

## دانلود

باینری‌ها را **خودِ GitHub Actions می‌سازد** و در صفحهٔ
[Releases](https://github.com/FossilizedProgrammer/lantern-headless-iran/releases) می‌گذارد —
**چیزی از روی هیچ کامپیوتری آپلود نمی‌شود**:

| فایل | سیستم‌عامل | چیست |
|---|---|---|
| `lantern-headless-linux-amd64` | لینوکس، x86_64 | خودِ CLI |
| `lantern-headless-windows-amd64.exe` | ویندوز، ۶۴ بیت | خودِ CLI |
| `lantern-radiance` | لینوکس، x86_64 | باینری کمکی موتور نسخهٔ ۱۰ |
| `lantern-radiance.exe` | ویندوز، ۶۴ بیت | باینری کمکی موتور نسخهٔ ۱۰ |

**برای `--engine radiance` فایل `lantern-radiance` (در ویندوز `lantern-radiance.exe`) را در همان
پوشهٔ CLI بگذارید.** CLI آن را کنار خودش (یا در `--radiance-engine` یا `$PATH`) جست‌وجو می‌کند.
`--engine flashlight` به آن نیازی ندارد.

چطور کار می‌کند: هر push به شاخهٔ `main` هر چهار باینری را می‌سازد (تا مطمئن شویم بیلد سالم است)،
و هر تگ نسخه (`v*`) علاوه بر آن یک **Release** با همان فایل‌ها می‌سازد.

## شروع سریع (radiance — پیشنهادی)

1. از **Releases** هم CLI و هم باینری کمکی `lantern-radiance` مناسب سیستم‌عامل خود را دانلود کنید
   و در یک پوشه بگذارید.
2. اجرا کنید — **به هیچ آپ‌استریمی نیاز نیست**:

**لینوکس**

```shell
chmod +x lantern-headless-linux-amd64 lantern-radiance
./lantern-headless-linux-amd64 --data-path ./data start --engine radiance \
  --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

**ویندوز (Command Prompt)**

```bat
lantern-headless-windows-amd64.exe --data-path data start --engine radiance --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

3. مرورگر را به پورت‌های محلی وصل کنید:

**Firefox** (توصیه‌شده چون DNS محلی معمولاً مسموم است):
`Settings → General → Network Settings → Manual proxy configuration` → SOCKS Host `127.0.0.1`،
پورت `18071`، نوع **SOCKS v5**، و گزینهٔ **"Proxy DNS when using SOCKS5"** را بزنید.

**Chrome/Chromium**

```shell
google-chrome --proxy-server="socks5://127.0.0.1:18071"
```

> کلاینت بدون رابط گرافیکی است و هیچ‌وقت تنظیمات سیستم را عوض نمی‌کند. اگر مرورگر را تنظیم
> نکنید، ترافیک مستقیم می‌رود و از فیلتر رد نمی‌شود.

### شروع سریع (flashlight — قدیمی، نیازمند آپ‌استریم)

اگر ترنسپورت قدیمی را ترجیح می‌دهید یا باینری کمکی radiance را ندارید، موتور flashlight را با یک
پروکسی سالم محلی (مثلاً Tor یا Psiphon) روی پورت `1080` اجرا کنید:

**لینوکس**

```shell
./lantern-headless-linux-amd64 --data-path ./data --upstream-proxy socks5://127.0.0.1:1080 \
  start --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

**ویندوز (Command Prompt)**

```bat
lantern-headless-windows-amd64.exe --data-path data --upstream-proxy socks5://127.0.0.1:1080 start --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

`--upstream-proxy` پروتکل‌های `socks5://`، `http://` و `https://` را می‌پذیرد. در موتور
flashlight همه‌کاره است: دریافت کانفیگ، بازکردن اتصال (معمولاً مسدود) به سرورهای لنترن، و مسیر
دادهٔ اضطراری. در `--engine radiance` اختیاری است و فقط برای بوت‌استرپ کنترل‌پلین به‌کار می‌رود.

## معنای پیام‌های راه‌اندازی

موتور radiance:

```
SUCCESS  Radiance engine connected to the Lantern network
INFO     Local proxy is listening — SOCKS5: 127.0.0.1:18071  HTTP: 127.0.0.1:18070
INFO     Verifying the data path with a real request through that proxy...
SUCCESS  Data path OK — HTTP 204 from https://www.google.com/generate_204 in 505ms
INFO     Traffic leaves this machine through IP 163.123.193.24
```

موتور flashlight:

```
SUCCESS  Control plane reachable (config fetched successfully)
INFO     Local proxy is listening — SOCKS5: 127.0.0.1:18071  HTTP: 127.0.0.1:18070
SUCCESS  Data path OK — HTTP 204 from https://www.google.com/generate_204 in 803ms
INFO     Traffic leaves this machine through IP 31.171.100.110
```

- **… connected / Control plane reachable** — اتصال برقرار شد؛ به‌تنهایی ربطی به ترافیک مرورگر
  ندارد.
- **Local proxy is listening** — پورت‌ها بازند (باز بودن ≠ کار کردن).
- **Data path OK / NOT working** — مهم‌ترین خط: کلاینت یک درخواست واقعی از پروکسی خودش زده است.
  اگر `NOT working` بود، خط `Hint:` دقیقاً می‌گوید چه کنید.
- **Traffic leaves this machine through IP …** — IP عمومی‌ای که مقصد می‌بیند.

## فرمان‌ها

| فرمان | کار |
|---|---|
| `start [--engine radiance\|flashlight]` | اجرای پروکسی محلی HTTP + SOCKS5 |
| `update [--via upstream\|direct\|auto] [--check] [--json]` | به‌روزرسانی فهرست ذخیره‌شدهٔ سرورها |
| `servers [--refresh] [--check] [--json]` | فهرست سرورها با کشور/شهر؛ `--check` دسترسی‌پذیری را نشان می‌دهد |
| `start --server <name>` | قفل‌کردن اتصال به یک سرور (بدون failover خودکار!) |
| `auth login / signup / logout` | ورود / ثبت‌نام در لنترن |
| `--help`, `--version` | راهنما و نسخه |

مثال‌ها (پرچم‌های سراسری **قبل از** فرمان و پرچم‌های `start` بعد از آن می‌آیند):

```shell
./lantern-headless-linux-amd64 --data-path ./data start --engine radiance
./lantern-headless-linux-amd64 --data-path ./data update --via upstream
./lantern-headless-linux-amd64 --data-path ./data servers --check
./lantern-headless-linux-amd64 --data-path ./data start --engine radiance --server sunny-tokyo-02
```

## گزینه‌های اصلی

| گزینه | توضیح |
|---|---|
| `--engine <radiance\|flashlight>` | موتور اتصال (پیش‌فرض `flashlight`)؛ برای برابری با نسخهٔ ۱۰ از `radiance` استفاده کنید |
| `--radiance-engine <path>` | مسیر باینری کمکی `lantern-radiance` (پیش‌فرض: کنار CLI، `LANTERN_RADIANCE_ENGINE` یا `$PATH`) |
| `--upstream-proxy` | پروکسی سالم شما: بوت‌استرپ کنترل‌پلین؛ موتور flashlight برای رسیدن به سرورهای مسدود به آن نیاز دارد |
| `--data-path` | محل ذخیرهٔ کانفیگ و لاگ‌ها (پیش‌فرض `~/.lantern-headless`) |
| `--server <name>` | قفل‌کردن `start` روی یک سرور (فهرست در `servers`) |
| `--with-proxyless` | فعال‌کردن اتصال مستقیم/هوشمند برای درخواست‌های کنترلی (خاموش به‌صورت پیش‌فرض) |
| `--debug` / `--quiet` / `--raw` | میزان لاگ |

فهرست کامل: `--help`.

## عیب‌یابی

- **`could not find the lantern-radiance helper binary`** — فایل `lantern-radiance` (در ویندوز
  `lantern-radiance.exe`) را از Releases دانلود و کنار CLI بگذارید، یا
  `--radiance-engine /path/to/lantern-radiance` را بدهید.
- **`--engine radiance` قبل از آماده‌شدن خارج می‌شود** — با `--debug` دوباره اجرا کنید تا لاگ خودِ
  موتور را ببینید؛ جزئیات در فایل لاگ داخل `--data-path` هم نوشته می‌شود.
- **`update` شکست خورد** — اتصال مستقیم از آلیاس Cloudflare (`df.getiantem.org`) استفاده می‌کند؛
  اگر آن هم مسدود بود `--upstream-proxy` را به یک پروکسی سالم وصل کنید. پیام `HTTP 401/403` یعنی
  اول `auth login` کنید.
- **`bind: address already in use`** — پورت اشغال است؛ پورت دیگری بدهید
  (`--http-proxy-addr` / `--socks-proxy-addr`).
- **سوکس وصل می‌شود ولی صفحه باز نمی‌شود (موتور flashlight)** — سرورهای لنترن از این شبکه قابل
  دسترس نیستند؛ `--upstream-proxy` را فعال کنید یا به `--engine radiance` بروید که خودش به آن‌ها
  می‌رسد.
- **دامنه‌ای به `10.x.x.x` resolve می‌شود** — DNS محلی مسموم است؛ کلاینت آن را تشخیص می‌دهد و نام
  دامنه را از سمت سرور resolve می‌کند (خط لاگ: `keeping the hostname so it is resolved remotely`).

## ساخت از سورس

خودِ CLI به [Go](https://go.dev/dl/) نسخهٔ ۱.۲۴ یا جدیدتر نیاز دارد:

```shell
go build -o bin/lantern .
./bin/lantern --help
```

موتور radiance در ماژول جداگانهٔ خودش (`radiance-engine/`) است و **باید با Go 1.26.x ساخته شود**
(Go 1.27 یک `//go:linkname` در sing-box را می‌شکند). با تارگت کمکی بسازید:

```shell
make radiance-engine   # خروجی: bin/lantern-radiance
```

در لینوکس از CGO استفاده می‌کند؛ برای بیلد کراس ویندوز از `CGO_ENABLED=0 GOOS=windows GOARCH=amd64
make radiance-engine` استفاده کنید. باینری حاصل را کنار CLI بگذارید.

## این فورک چه فرقی با آپ‌استریم دارد؟

- **موتور radiance (`--engine radiance`)**: برابری کامل با کلاینت رسمی لنترن نسخهٔ ۱۰ — همان
  دیتاپلین Radiance + lantern-box، با تگ `novpn` ساخته شده تا به‌جای دستگاه TUN یک پروکسی
  HTTP/SOCKS بدهد. خودش با مقاوم‌سازی مدرن در برابر سانسور وصل می‌شود، پس در شبکهٔ فیلترشده بدون
  آپ‌استریم کار می‌کند.
- **آلیاس Cloudflare برای صفحهٔ کنترل**: درخواست‌های کانفیگ/ورود به‌جای `df.iantem.io` به
  `df.getiantem.org` می‌روند، چون لبهٔ گوگل به IPهای ایران `403` می‌دهد؛ همان آلیاسی که خودِ
  کلاینت Radiance لنسرن برای مناطق محدود استفاده می‌کند.
- **`--upstream-proxy` برای موتور flashlight**: بازکردن اتصال TCP (معمولاً مسدود) به سرورهای
  لنسرن و مسیر داده به‌عنوان آخرین راه‌حل — پورت‌های محلی جواب می‌دهند و ترافیک می‌تواند از شبکهٔ
  لنسرن خارج شود، حتی وقتی هیچ سروری مستقیم در دسترس نیست.
- **رفع DNS مسموم**: IP خصوصیِ برگشتی از DNS محلی هرگز مستقیم گرفته نمی‌شود؛ نام دامنه نگه داشته
  می‌شود تا سرور خودش resolve کند.
- **فرمان‌های `servers` و `update`**: فهرست سرورها با موقعیت، تست دسترسی، و قفل‌کردن `start` به
  یک سرور.
- **تشخیص صادقانهٔ وضعیت**: قبل از اعلام سالم‌بودن، یک درخواست واقعی از پروکسی زده می‌شود.

## مجوز

[Apache License 2.0](./LICENSE) — مانند آپ‌استریم. لنسرن علامت تجاری مالکانش است؛ این پروژه یک
فورک مستقل و غیررسمی است.
