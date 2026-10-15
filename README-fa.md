# lantern-headless-iran

[English](./README.md) · **فارسی** · [Русский](./README-ru.md)

> فورک **غیررسمی** از [`getlantern/lantern-headless-client`](https://github.com/getlantern/lantern-headless-client).
> هیچ وابستگی به تیم لنسرن ندارد.

کلاینت لنسرن **بدون رابط گرافیکی**، ساخته‌شده برای شبکه‌های با فیلترینگ شدید (تست‌شده در ایران).
کلاینت دو پورت محلی باز می‌کند و مرورگر شما به آن‌ها وصل می‌شود؛ ترافیک از شبکهٔ لنسرن بیرون می‌رود.

## کی به‌درد می‌خورد و کی نه

- **در مناطقی با فیلترینگ شدید که سرورهای خودِ لنسرن مسدود هستند، این برنامه به‌تنهایی برای
  عبور از فیلترینگ به‌درد نمی‌خورد.** بدون یک `--upstream-proxy` که از قبل کار کند، اتصال‌هایش
  صرفاً شکست می‌خورند — این ابزاری برای عبور یک‌کلیکی از فیلتر نیست.
- **کاربردش در چنین شرایطی: گرفتن یک IP مناسب از مسیر آپ‌استریم است.** فقط همان اتصال کوتاه به
  سرورهای مسدودِ لنسرن از پروکسی سالم شما می‌رود؛ ترافیک شما روی پروتکل خودِ لنسرن سوار می‌ماند
  و با یک IP خروجی مناسب بیرون می‌رود.
- پس دستورِ کار برای چنین شبکه‌هایی: یک پروکسی که از قبل کار می‌کند (Tor، Psiphon، رله و…) داشته
  باشید و از این کلاینت برای گرفتن IP مناسب از همان آپ‌استریم استفاده کنید.

## دانلود

باینری‌ها را **خودِ GitHub Actions می‌سازد** و در صفحهٔ
[Releases](https://github.com/FossilizedProgrammer/lantern-headless-iran/releases) می‌گذارد —
**چیزی از روی هیچ کامپیوتری آپلود نمی‌شود**:

| فایل | سیستم‌عامل |
|---|---|
| `lantern-headless-linux-amd64` | لینوکس، x86_64 |
| `lantern-headless-windows-amd64.exe` | ویندوز، ۶۴ بیت |

چطور کار می‌کند: هر push به شاخهٔ `main` هر دو باینری را می‌سازد (تا مطمئن شویم بیلد سالم است)، و
هر تگ نسخه (`v*`) علاوه بر آن یک **Release** با همان دو فایل می‌سازد.

## شروع سریع

1. فایل مربوط به سیستم‌عامل خود را از **Releases** دانلود کنید.
2. اجرا کنید. مثال زیر فرض می‌کند یک پروکسی سالم محلی (مثلاً Tor یا Psiphon) روی پورت `1080` دارید:

**لینوکس**

```shell
chmod +x lantern-headless-linux-amd64
./lantern-headless-linux-amd64 --data-path ./data --upstream-proxy socks5://127.0.0.1:1080 \
  start --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

**ویندوز (Command Prompt)**

```bat
lantern-headless-windows-amd64.exe --data-path data --upstream-proxy socks5://127.0.0.1:1080 start --http-proxy-addr 127.0.0.1:18070 --socks-proxy-addr 127.0.0.1:18071
```

`--upstream-proxy` اختیاری است اما در شبکهٔ فیلترشده بشدت توصیه می‌شود. پروتکل‌های `socks5://`،
`http://` و `https://` را می‌پذیرد و همه‌کاره است: دریافت کانفیگ، بازکردن اتصال به خودِ سرورهای
لنسرن (که IPشان معمولاً مسدود است) و در نهایت مسیر داده به‌عنوان آخرین راه‌حل.

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

## معنای پیام‌های راه‌اندازی

```
SUCCESS  Control plane reachable (config fetched successfully)
INFO     Local proxy is listening — SOCKS5: 127.0.0.1:18071  HTTP: 127.0.0.1:18070
SUCCESS  Data path OK — HTTP 204 from https://www.google.com/generate_204 in 803ms
INFO     Traffic leaves this machine through IP 31.171.100.110
```

- **Control plane reachable** — کانفیگ دریافت شد؛ ربطی به ترافیک مرورگر ندارد.
- **Local proxy is listening** — پورت‌ها بازند (باز بودن ≠ کار کردن).
- **Data path OK / NOT working** — مهم‌ترین خط: کلاینت یک درخواست واقعی از پروکسی خودش زده است.
  اگر `NOT working` بود، خط `Hint:` دقیقاً می‌گوید چه کنید.
- **Traffic leaves this machine through IP …** — IP عمومی‌ای که مقصد می‌بیند.

## فرمان‌ها

| فرمان | کار |
|---|---|
| `start` | اجرای پروکسی محلی HTTP + SOCKS5 |
| `update [--via upstream\|direct\|auto] [--check] [--json]` | به‌روزرسانی فهرست ذخیره‌شدهٔ سرورها |
| `servers [--refresh] [--check] [--json]` | فهرست سرورها با کشور/شهر؛ `--check` دسترسی‌پذیری را نشان می‌دهد |
| `start --server <name>` | قفل‌کردن اتصال به یک سرور (بدون failover خودکار!) |
| `auth login / signup / logout` | ورود / ثبت‌نام در لنسرن |
| `--help`, `--version` | راهنما و نسخه |

مثال‌ها (پرچم‌های سراسری **قبل از** فرمان می‌آیند):

```shell
./lantern-headless-linux-amd64 --data-path ./data update --via upstream
./lantern-headless-linux-amd64 --data-path ./data servers --check
./lantern-headless-linux-amd64 --data-path ./data start --server sunny-tokyo-02
```

## گزینه‌های اصلی

| گزینه | توضیح |
|---|---|
| `--upstream-proxy` | پروکسی سالم شما: دریافت کانفیگ، اتصال به سرورهای لنسرن، مسیر دادهٔ اضطراری |
| `--data-path` | محل ذخیرهٔ کانفیگ و لاگ‌ها (پیش‌فرض `~/.lantern-headless`) |
| `--server <name>` | قفل‌کردن `start` روی یک سرور (فهرست در `servers`) |
| `--with-proxyless` | فعال‌کردن اتصال مستقیم/هوشمند برای درخواست‌های کنترلی (خاموش به‌صورت پیش‌فرض) |
| `--debug` / `--quiet` / `--raw` | میزان لاگ |

فهرست کامل: `--help`.

## عیب‌یابی

- **`update` شکست خورد** — اول `--via direct` را امتحان کنید؛ اگر جواب نداد `--upstream-proxy` را
  به یک پروکسی سالم وصل کنید (اتصال مستقیم به سرویس کانفیگ معمولاً مسدود است). پیام
  `HTTP 401/403` یعنی اول `auth login` کنید.
- **`bind: address already in use`** — پورت اشغال است؛ پورت دیگری بدهید
  (`--http-proxy-addr` / `--socks-proxy-addr`).
- **سوکس وصل می‌شود ولی صفحه باز نمی‌شود** — سرورهای لنسرن از این شبکه قابل دسترس نیستند؛
  `--upstream-proxy` را فعال کنید. اگر در لاگ `Fell back to the upstream proxy` می‌بینید، ترافیک
  از پروکسی خودتان خارج می‌شود نه از شبکهٔ لنسرن.
- **دامنه‌ای به `10.x.x.x` resolve می‌شود** — DNS محلی مسموم است؛ کلاینت آن را تشخیص می‌دهد و نام
  دامنه را از سمت سرور resolve می‌کند (خط لاگ: `keeping the hostname so it is resolved remotely`).

## ساخت از سورس

نیاز به [Go](https://go.dev/dl/) نسخهٔ ۱.۲۴ یا جدیدتر:

```shell
go build -o bin/lantern .
./bin/lantern --help
```

## این فورک چه فرقی با آپ‌استریم دارد؟

- **`--upstream-proxy` همه‌کاره است**: دریافت کانفیگ، بازکردن اتصال TCP (معمولاً مسدود) به
  سرورهای لنسرن، و مسیر داده به‌عنوان آخرین راه‌حل — پورت‌های محلی حتی وقتی هیچ سرور لنسرنی
  مستقیم در دسترس نیست جواب می‌دهند.
- **رفع DNS مسموم**: IP خصوصیِ برگشتی از DNS محلی هرگز مستقیم گرفته نمی‌شود؛ نام دامنه نگه داشته
  می‌شود تا سرور خودش resolve کند.
- **فرمان‌های `servers` و `update`**: فهرست سرورها با موقعیت، تست دسترسی، و قفل‌کردن `start` به
  یک سرور.
- **تشخیص صادقانهٔ وضعیت**: قبل از اعلام سالم‌بودن، یک درخواست واقعی از پروکسی زده می‌شود.

## مجوز

[Apache License 2.0](./LICENSE) — مانند آپ‌استریم. لنسرن علامت تجاری مالکانش است؛ این پروژه یک
فورک مستقل و غیررسمی است.
