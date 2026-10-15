# کلاینت لنسرن بدون رابط گرافیکی

این فورک از [`lantern-headless-client`](https://github.com/getlantern/lantern-headless-client) است: کلاینت با
`--upstream-proxy` به کانفیگ و سرورهای لنسرن می‌رسد و ترافیک کاربر را از شبکهٔ لنسرن عبور می‌دهد.

## تفاوت با آپ‌استریم

- **smart dialer پیش‌فرض خاموش است** (با `--with-proxyless` روشن می‌شود).
- **گزینه `--upstream-proxy` حالا سه کار انجام می‌دهد** (مثلاً `socks5://127.0.0.1:1080`):
  ۱) کانفیگ‌ها را از طریق آن می‌گیرد،
  ۲) **اتصال خام TCP به خودِ سرورهای لنسرن** را از مسیر آن باز می‌کند — سرورهای لنسرن در شبکه‌های فیلترشده
  معمولاً IPشان مسدود است، ولی از خروجی یک پروکسی سالم (Tor/psiphon/رله کلادفلر) قابل‌دسترس‌اند؛ فقط همین
  اتصال کوتاه از upstream می‌رود و ترافیک کاربر روی پروتکل خودِ لنسرن (tlsmasq/https/...) سوار می‌شود و از
  شبکه لنسرن خارج می‌شود،
  ۳) اگر هیچ سرور لنسرنی در دسترس نبود، **به‌عنوان آخرین راه‌حل** ترافیک مرورگر را از همان پروکسی عبور می‌دهد
  تا پورت SOCKS/HTTP هیچ‌وقت قطع نشود. اگر upstream تعریف نشده باشد، هیچ‌کدام از این‌ها فعال نیست و رفتار
  پیش‌فرض حفظ می‌شود.
- **رفع DNS مسموم**: اگر DNS محلی یک دامنه عمومی را به IP خصوصی برگرداند (مثلاً `youtube.com → 10.10.34.36`)،
  کلاینت دیگر آن IP را مستقیم نمی‌زند و نام دامنه را نگه می‌دارد تا سرور پروکسی (یا upstream) خودش resolve کند؛
  قبلاً چنین درخواست‌هایی با `connection reset` شکست می‌خوردند.
- **فرمان جدید `servers` و پرچم `--server`**: فهرست همهٔ سرورهای کانفیگ به‌همراه کشور و شهرشان، و امکان
  قفل‌کردن اتصال به یک سرور مشخص (به‌جای انتخاب خودکار توسط multi-armed bandit) — توضیح در بخش
  «فهرست سرورها و اتصال به یک سرور مشخص».
- **فرمان جدید `update`**: به‌روزرسانی دستی فهرست سرورها با انتخاب روش اتصال — آپ‌استریم، اتصال مستقیم یا
  خودکار (`--via upstream|direct|auto`). نتیجه در `user.conf` ذخیره می‌شود و `start` بلافاصله از آن
  استفاده می‌کند.

## پیش‌نیازها

- Go 1.24 یا جدیدتر
- دسترسی به ماژول‌ها: اگر دسترسی مستقیم به `proxy.golang.org` ندارید:

```shell
go env -w GOPROXY=https://proxy.golang.org,https://goproxy.cn,direct
```

## ساخت

```shell
go build -o bin/lantern .
# یا برای باینری نسخه‌دار:
make build-internal PLATFORM=linux ARCH=amd64 IMAGE=none VERSION=1.0.0
```

اجرای محلی بدون نصب:

```shell
./bin/lantern --help
```

## گردش کار پیشنهادی

### ۱) به‌روزرسانی فهرست سرورها

فرمان `update` فهرست سرورها را همان لحظه از سرویس کانفیگ لنسرن می‌گیرد و در
`<data-path>/user.conf` ذخیره می‌کند تا `start` (و `servers`) بلافاصله از آن استفاده کنند. روش اتصال را
خودتان انتخاب می‌کنید:

```shell
./bin/lantern update --via upstream   # از طریق --upstream-proxy
./bin/lantern update --via direct     # اتصال مستقیم
./bin/lantern update                  # auto (پیش‌فرض): آپ‌استریم (اگر تعریف شده) ← مستقیم
./bin/lantern update --json           # خروجی JSON
./bin/lantern update --check          # تست دسترسی‌پذیری هر سرور
```

با `--check` بعد از دریافت لیست، هر سرور به‌صورت موازی پروب TCP می‌شود و در جدول ستون `Reachable`
اضافه می‌شود (`open` = پورت ۴۴۳ باز؛ `unreachable` = به احتمال زیاد IP بلاک شده). سرورهای unreachable
از طریق `--upstream-proxy` همچنان می‌توانند کار کنند. همین قابلیت برای لیست کش‌شده هم موجوده:
`servers --check` (و `--json` فیلد `reachable` را می‌دهد).

| پرچم | پیش‌فرض | توضیح |
|---|---|---|
| `--via` | `auto` | روش اتصال به سرویس کانفیگ: `upstream`، `direct` یا `auto` (به‌ترتیب: آپ‌استریم اگر تعریف شده، سپس مستقیم) |
| `--timeout` | `45` | timeout هر تلاش (ثانیه) |
| `--json` | خاموش | چاپ فهرست سرورهای دریافتی به‌صورت JSON |
| `--check` | خاموش | پروب TCP هر سرور و نمایش وضعیت دسترسی مستقیم (ستون Reachable) |
| `--config-url` | `https://df.iantem.io/api/v1/config` | URL جایگزین سرویس کانفیگ |

اگر همهٔ روش‌ها شکست بخورند، `--upstream-proxy` را به یک پروکسی سالم (Tor/psiphon/رله کلادفلر) وصل
کنید — اتصال مستقیم به `df.iantem.io` در ایران معمولاً مسدود است. برای `--via upstream` تعریف‌کردن
`--upstream-proxy` الزامی است.

### ۲) اجرای کلاینت

```shell
./bin/lantern --data-path data --upstream-proxy socks5://127.0.0.1:1080 start \
  --http-proxy-addr 127.0.0.1:18080 --socks-proxy-addr 127.0.0.1:18081
```

### پیام‌های راه‌اندازی و معنای واقعی آن‌ها

```
SUCCESS  Control plane reachable (config fetched successfully)
INFO     Local proxy is listening — SOCKS5: 127.0.0.1:18071  HTTP: 127.0.0.1:18070
INFO     Verifying the data path with a real request through that proxy...
SUCCESS  Data path OK — HTTP 204 from https://www.google.com/generate_204 in 803ms
INFO     Traffic leaves this machine through IP 31.171.100.110
```

- **`Control plane reachable`** فقط یعنی کانفیگ با موفقیت دریافت شده؛ ربطی به ترافیک مرورگر ندارد.
- **`Local proxy is listening`** فقط یعنی پورت‌ها bind شده‌اند (گوش دادن ≠ کار کردن).
- **`Data path OK` / `Data path is NOT working`** تنها خطی است که وضعیت واقعی مرورگر را می‌گوید: کلاینت
  واقعاً یک درخواست از خودِ پروکسی محلی می‌زند (۲ هدف × ۳ تا با وقفهٔ ۵ ثانیه). اگر این خط `NOT working`
  بود یعنی پورت‌ها بازند ولی هیچ درخواستی بیرون نمی‌رود — پیام `Hint:` بعد از آن دقیقاً می‌گوید چه کنی.
- **`Traffic leaves this machine through IP ...`** IP عمومی‌ای است که مقصد می‌بیند؛ با آن می‌فهمی خروجی از
  شبکه لنسرن است یا از upstream.

### فهرست سرورها و اتصال به یک سرور مشخص

فرمان `servers` همهٔ سرورهای موجود در کانفیگ را با موقعیت جغرافیایی (همان‌طور که خود کانفیگ لنسرن
اعلام می‌کند) نشان می‌دهد:

```shell
./bin/lantern --data-path data servers            # از نسخهٔ کش‌شدهٔ <data-path>/user.conf
./bin/lantern --data-path data servers --refresh  # واکشی تازه از سرویس کانفیگ (مثل start)
./bin/lantern --data-path data servers --json     # خروجی ماشین‌خوان
```

خروجی:

```
Name              | Address          | Protocol | Country       | City
flaky-frankfurt-01| 198.51.100.10:443| https    | DE Germany    | Frankfurt
sunny-tokyo-02    | 203.0.113.10:443 | https    | JP Japan      | Tokyo
```

برای قفل‌کردن اتصال به یک سرور مشخص، نام آن را به `start` بدهید (تطبیق نام بزرگی/کوچکی حروف را
نادیده می‌گیرد؛ قبل از ساخته‌شدن هر dialer، بقیهٔ سرورها از کانفیگ حذف می‌شوند، پس انتخابگر داخلی
دیگر راهی جز همان سرور ندارد):

```shell
./bin/lantern --data-path data start --server sunny-tokyo-02 \
  --http-proxy-addr 127.0.0.1:18080 --socks-proxy-addr 127.0.0.1:18081
```

کلاینت وقتی سرور در کانفیگ ظاهر شد پیام `Using pinned server "sunny-tokyo-02" (Tokyo, Japan)` را
چاپ می‌کند و اگر نام در لیست نباشد، هشدار می‌دهد و نام‌های موجود را نشان می‌دهد.

⚠️ **مبادلهٔ مهم**: با پین‌کردن، failover از بین می‌رود — اگر آن سرور بلاک یا از کار بیفتد، اتصال
کار نمی‌کند تا یا سرور دیگری انتخاب کنید یا پرچم را بردارید.

## پرچم‌های سراسری

| پرچم | توضیح |
|---|---|
| `--data-path` | مسیر نگهداری دیتای کلاینت (کانفیگ و لاگ) |
| `--upstream-proxy` | پروکسی سالم (`socks5://` یا `http://`) برای دریافت کانفیگ، اتصال به سرورهای لنسرن و fallback داده؛ مثلاً `socks5://127.0.0.1:1080` |
| `--with-proxyless` | فعال‌کردن smart dialer برای درخواست‌های کنترلی (خاموش به‌صورت پیش‌فرض) |
| `--server <name>` | قفل‌کردن `start` به یک سرور مشخص بر اساس نام (بخش «فهرست سرورها») |
| `update [--via …]` | به‌روزرسانی فهرست سرورها با روش دلخواه (آپ‌استریم / مستقیم / خودکار) |
| `--debug` | لاگ debug |
| `--quiet` / `--raw` | کنترل خروجی لاگ |

## عیب‌یابی

- **`update` شکست خورد**: اول `--via direct` را امتحان کنید؛ اگر جواب نداد `--upstream-proxy` را به یک
  پروکسی سالم وصل کنید — اتصال مستقیم به `df.iantem.io` در ایران معمولاً مسدود است. پیام
  `HTTP 401/403` یعنی اول `auth login` کنید.
- **`bind: address already in use`**: پورت پروکسی اشغال است؛ پورت دیگری بدهید (`--http-proxy-addr` /
  `--socks-proxy-addr`)؛ مقادیر `:0` پورت تصادفی می‌دهند.
- **سوکس وصل می‌شود ولی صفحه باز نمی‌شود / `connection reset`**: یعنی سرورهای لنسرن از این شبکه قابل‌دسترس
  نیستند. لاگ را ببینید: اگر `i/o timeout` روی IP سرورهای لنسرن دارید، `--upstream-proxy` به یک پروکسی سالم
  وصل است (آن را فعال کنید). اگر `Fell back to the upstream proxy` می‌بینید، یعنی هیچ سرور لنسرنی پاسخ نداد و
  ترافیک از upstream می‌رود — پورت کار می‌کند ولی خروجی از لنسرن نیست.
- **دامنه‌ای مثل `youtube.com` به `10.10.34.36` resolve می‌شود**: DNS محلی مسموم است؛ کلاینت حالا نام دامنه را
  نگه می‌دارد و resolve از سمت سرور پروکسی انجام می‌شود. برای اطمینان در لاگ دنبال
  `keeping the hostname so it is resolved remotely` بگردید.
- **دریافت کانفیگ طول می‌کشد**: اگر هیچ‌وقت تمام نشد، لاگ‌ها را با `--debug` بررسی کنید و `--via direct`
  را امتحان کنید.

## ساختار مخزن

- `third_party/flashlight/` — نسخه محلی و پچ‌شده ماژول آپ‌استریم
  (`go.mod` با `replace` به آن اشاره می‌کند)
- `update.go`, `servers.go`, `cli.go`, `clientopts.go` — فرمان‌های `update` و `servers`، پرچم
  `--server` و گزینه‌های سراسری

## مجوز

مانند آپ‌استریم: [Apache License 2.0](./LICENSE)
