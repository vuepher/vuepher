# PRD Vuepher: Full-Stack MVC Framework Go + Vue 3

Versi 0.1 (Draft untuk review) · 16 Oktober 2026 · Lisensi rencana: MIT

## 1. Executive Summary & Problem Statement

### 1.1 Ringkasan

Vuepher adalah framework web full-stack open-source berbasis Go dengan arsitektur MVC, menggunakan Vue 3 sebagai view layer. Routing, validasi, autentikasi, dan akses data sepenuhnya berada di Go. Controller Go me-render komponen Vue lewat protokol yang kompatibel secara konsep dengan Inertia.js, sehingga developer mendapatkan pengalaman SPA tanpa membangun dan memelihara REST API terpisah. Seluruh aplikasi (backend dan hasil build frontend) dapat dikompilasi menjadi satu binary tunggal melalui `go:embed`.

Visi: *kenyamanan Developer Experience (DX) ala Laravel, dengan performa dan kemudahan deployment ala Go.*

### 1.2 Problem Statement

| Masalah | Dampak pada developer |
| --- | --- |
| Ekosistem Go didominasi library kecil yang harus dirangkai sendiri (router, ORM, validator, migrasi, session). | Setiap proyek baru memulai dari keputusan arsitektur yang sama, tanpa konvensi bersama. |
| Go tidak punya generator/scaffolding setara Artisan. | Banyak boilerplate ditulis manual dan struktur proyek tidak konsisten antar tim. |
| Memadukan Go dengan frontend modern biasanya berarti dua repositori, REST/GraphQL API, CORS, dan duplikasi tipe data. | Kompleksitas tinggi untuk aplikasi yang sebenarnya sederhana (CRUD, dashboard, SaaS kecil). |
| Developer Laravel yang ingin pindah ke Go kehilangan konvensi, generator, dan alur Inertia. | Hambatan migrasi tinggi, kurva belajar curam. |
| Tipe data di backend dan frontend sering tidak sinkron. | Bug runtime yang seharusnya tertangkap saat kompilasi. |

### 1.3 Solusi

Vuepher menyediakan satu framework "batteries-included" dengan konvensi jelas: CLI generator, MVC berlapis, ORM dengan sintaks fluent, jembatan Go↔Vue tanpa API terpisah, auto type-generation Go→TypeScript, dan deployment satu file binary.

## 2. Tujuan, Non-Tujuan, dan Metrik Keberhasilan

### 2.1 Tujuan

1. Developer dapat membuat proyek baru dan melihat halaman Vue yang dirender dari controller Go dalam waktu kurang dari 5 menit.
2. Menyediakan generator yang menghilangkan boilerplate harian (controller, model, migrasi, middleware, view).
3. Menghilangkan kebutuhan REST API terpisah untuk aplikasi monolitik.
4. Memberikan type-safety end-to-end dari struct Go sampai props komponen Vue.
5. Menghasilkan satu binary siap deploy tanpa dependensi runtime Node.js.

### 2.2 Non-Tujuan (di luar scope v1.0)

- Server-Side Rendering (SSR) Vue. Rencana evaluasi pasca-v1.0.
- Dukungan view layer selain Vue (React, Svelte).
- Membangun ORM baru dari nol. Vuepher membungkus GORM.
- Fitur enterprise (multi-tenancy bawaan, broadcasting skala besar).
- Kompatibilitas kode dengan PHP/Laravel. Yang diadopsi adalah konvensi dan DX, bukan sintaks.

### 2.3 Metrik Keberhasilan

| Metrik | Target 12 bulan setelah v1.0 |
| --- | --- |
| Waktu `vuepher new` → halaman pertama tampil | < 5 menit (termasuk instalasi dependensi) |
| GitHub stars | 2.000+ |
| Kontributor aktif (≥1 PR merged) | 30+ |
| Aplikasi produksi yang dilaporkan komunitas | 50+ |
| Cakupan dokumentasi fitur publik | 100% |
| Skor kepuasan survei DX (skala 1–5) | ≥ 4,2 |

## 3. Target Pengguna (Persona)

**Persona A: Rizal, Developer Laravel/PHP (migrator)** Pengalaman 4–6 tahun di Laravel + Inertia + Vue. Ingin performa dan biaya hosting lebih rendah serta deployment sederhana. Mengharapkan `make:*`, migrasi, validasi form, flash message, dan middleware yang familier. Frustrasi dengan boilerplate Go dan tidak adanya konvensi.

**Persona B: Dewi, Full-stack Developer (Go + Vue)** Nyaman dengan Go dan Vue 3, bekerja di tim kecil atau startup. Ingin satu repositori monolitik, tanpa API terpisah, tanpa sinkronisasi tipe manual, dan deploy berupa satu binary di VPS atau container kecil.

## 4. User Stories & Use Cases

### 4.1 User Stories

| ID | Sebagai... | Saya ingin... | Agar... |
| --- | --- | --- | --- |
| US-01 | Developer | menjalankan `vuepher new blog` | mendapat proyek Go + Vite + Vue 3 yang langsung berjalan |
| US-02 | Developer | menjalankan `vuepher dev` | Go dan Vite HMR berjalan paralel dan perubahan kode langsung terlihat |
| US-03 | Developer | menjalankan `vuepher make:model Post -m` | model dan file migrasi dibuat otomatis |
| US-04 | Developer | menulis `ctx.View("Posts/Index", props)` di controller | halaman Vue tampil tanpa membuat endpoint JSON |
| US-05 | Developer | memakai `<VuepherLink>` | navigasi antar halaman terasa seperti SPA tanpa reload penuh |
| US-06 | Developer | memakai `useVuepherForm()` | state form, status pengiriman, dan error validasi dari Go ditangani otomatis |
| US-07 | Developer | mendefinisikan struct Go sebagai props | komponen Vue mendapat auto-complete TypeScript akurat |
| US-08 | Developer | menulis query dengan `Post::Where(...).With(...).Paginate()` | akses data terasa seperti Eloquent |
| US-09 | Developer | menjalankan `vuepher build` | mendapat satu binary untuk di-deploy |
| US-10 | Developer | membuat middleware autentikasi dan melindungi grup route | akses halaman tertentu dibatasi |
| US-11 | Maintainer | menyesuaikan stub generator | output generator sesuai standar tim |

### 4.2 Use Case Utama: Membuat CRUD Post

1. `vuepher new blog && cd blog`
2. `vuepher make:model Post -m` → edit migrasi (title, body) → `vuepher migrate`
3. `vuepher make:controller PostController --resource`
4. `vuepher make:view Posts/Index` dan `vuepher make:view Posts/Create`
5. Mendaftarkan `router.Resource("posts", &controllers.PostController{})`
6. Menjalankan `vuepher dev`, membuka browser, menambah post lewat form tanpa reload penuh.

Contoh controller target:

```go
func (c *PostController) Index(ctx *vuepher.Context) error {
    posts, err := models.Post{}.Query().Latest().Paginate(ctx, 15)
    if err != nil {
        return err
    }
    return ctx.View("Posts/Index", vuepher.Props{"posts": posts})
}

func (c *PostController) Store(ctx *vuepher.Context) error {
    var req requests.StorePost
    if err := ctx.Validate(&req); err != nil {
        return ctx.BackWithErrors(err) // error otomatis tampil di useVuepherForm()
    }
    models.Create(&models.Post{Title: req.Title, Body: req.Body})
    return ctx.Redirect("/posts").With("success", "Post dibuat")
}
```

## 5. Arsitektur & Tech Stack Inti

| Lapisan | Teknologi | Catatan |
| --- | --- | --- |
| HTTP Router | Chi (rekomendasi) atau Fiber | Lihat keputusan 5.1 |
| ORM | GORM + wrapper fluent | Sintaks ala Active Record/Eloquent |
| View layer | Vue 3 + Vite + TypeScript | Protokol mirip Inertia.js |
| CLI | Cobra + `text/template` | Stub generator dapat di-publish dan dikustomisasi |
| Packaging | `go:embed` | Satu binary, aset ter-embed |
| Frontend package | `@vuepher/vue3` (npm) | `<VuepherLink>`, `useVuepherForm()`, bootstrap app |

### 5.1 Keputusan Router: Chi vs Fiber

Fiber berbasis `fasthttp`, sehingga tidak kompatibel langsung dengan `net/http` (middleware, `http.FS` untuk `go:embed`, `httptest`, ekosistem OpenTelemetry). Chi berbasis `net/http` standar, ringan, dan kompatibel dengan seluruh ekosistem. **Rekomendasi: Chi sebagai default**, dengan router dibungkus interface internal `vuepher.Router` agar adapter Fiber dapat ditambahkan di fase berikutnya bila ada kebutuhan throughput ekstrem. Keputusan final ditetapkan setelah benchmark pada Phase 1.

### 5.2 Alur Request (View Bridge)

1. Browser meminta halaman → Go merender HTML root berisi `<div id="app" data-page="{...}">` beserta tag aset (Vite dev server saat development, manifest hasil build saat produksi).
2. Klik `<VuepherLink>` mengirim request XHR/fetch dengan header `X-Vuepher: true` dan `X-Vuepher-Version`.
3. Go membalas JSON *page object*: `{ component, props, url, version }`.
4. Frontend menukar komponen halaman tanpa reload penuh dan memperbarui riwayat browser.
5. Jika versi aset berbeda, Go membalas `409` dengan `X-Vuepher-Location` sehingga browser melakukan reload penuh.
6. Redirect setelah `PUT/PATCH/DELETE` memakai status `303`.
7. Error validasi dikirim sebagai props `errors` pada halaman berikutnya (via session flash), bukan sebagai respons JSON terpisah.

## 6. Functional Requirements

Prioritas: **P0** = wajib MVP, **P1** = wajib v1.0, **P2** = pasca-v1.0.

### 6.1 Modul Core HTTP

| ID | Kebutuhan | Prioritas |
| --- | --- | --- |
| HTTP-01 | Router dengan method GET/POST/PUT/PATCH/DELETE, parameter path, dan grup route dengan prefix | P0 |
| HTTP-02 | `router.Resource()` untuk route CRUD standar (index, create, store, show, edit, update, destroy) | P0 |
| HTTP-03 | Named routes dan helper pembuat URL (`route("posts.show", id)`) yang juga tersedia di frontend | P1 |
| HTTP-04 | Pipeline middleware global, per-grup, dan per-route | P0 |
| HTTP-05 | Middleware bawaan: recover, logger, CORS, CSRF, session, rate limiter | P0 (recover, logger), P1 (sisanya) |
| HTTP-06 | `vuepher.Context` pembungkus request/response: input, query, file upload, cookie, header, JSON | P0 |
| HTTP-07 | Validasi request berbasis struct tag (`validate:"required,min=3"`) dengan pesan error yang dapat dilokalisasi | P0 |
| HTTP-08 | Session (cookie terenkripsi, driver memory/database/redis) dan flash data | P0 (cookie), P1 (driver lain) |
| HTTP-09 | Autentikasi bawaan: guard session, hashing password (bcrypt/argon2), `ctx.Auth().User()` | P1 |
| HTTP-10 | Konfigurasi via file `.env` + `config/*.go` dengan tipe kuat | P0 |
| HTTP-11 | Graceful shutdown, health-check endpoint, penanganan error terpusat (halaman error Vue untuk 404/500) | P1 |
| HTTP-12 | Service container / dependency injection sederhana untuk controller | P1 |
| HTTP-13 | Queue/job dan scheduler | P2 |

### 6.2 Modul ORM / Database

| ID | Kebutuhan | Prioritas |
| --- | --- | --- |
| DB-01 | Koneksi GORM dengan driver SQLite, PostgreSQL, MySQL dikonfigurasi lewat `.env` | P0 |
| DB-02 | Base model `vuepher.Model` (ID, CreatedAt, UpdatedAt, DeletedAt untuk soft delete) | P0 |
| DB-03 | Wrapper fluent: `Find`, `Where`, `OrderBy`, `Latest`, `First`, `Create`, `Update`, `Delete`, `Paginate` | P1 |
| DB-04 | Relasi (hasOne, hasMany, belongsTo, manyToMany) dengan eager loading `With("Comments")` | P1 |
| DB-05 | Sistem migrasi berversi: `migrate`, `migrate:rollback`, `migrate:fresh`, `migrate:status` | P0 |
| DB-06 | Seeder dan factory untuk data uji (`make:seeder`, `db:seed`) | P1 |
| DB-07 | Scope kueri, accessor/mutator, dan hook model (BeforeCreate, dst.) | P1 |
| DB-08 | Paginator yang serialisasi-nya langsung cocok dengan komponen pagination frontend | P1 |
| DB-09 | Transaksi helper `DB.Transaction(func(tx) error)` | P0 |
| DB-10 | Akses ke instance `*gorm.DB` mentah (escape hatch) tanpa batasan | P0 |

Prinsip desain: wrapper bersifat *opt-in* dan tipis. Developer tidak dikunci; GORM murni selalu dapat dipakai bersamaan.

### 6.3 Modul View Bridge (Go↔Vue)

| ID | Kebutuhan | Prioritas |
| --- | --- | --- |
| VIEW-01 | `ctx.View(component, props)` menghasilkan HTML penuh pada kunjungan pertama dan JSON page object pada request SPA | P0 |
| VIEW-02 | Template root HTML (`resources/views/app.html`) yang dapat dikustomisasi (title, meta, aset) | P0 |
| VIEW-03 | Integrasi Vite: tag dev (`@vite/client`) saat development, pembacaan `manifest.json` saat produksi | P0 |
| VIEW-04 | Deteksi versi aset dan penanganan `409` untuk reload otomatis | P1 |
| VIEW-05 | Shared props global (user terautentikasi, flash message, CSRF token) lewat middleware `ShareProps` | P0 |
| VIEW-06 | Partial reload (`only`/`except`) dan lazy props yang hanya dievaluasi bila diminta | P1 |
| VIEW-07 | `ctx.Redirect()`, `ctx.Back()`, `ctx.BackWithErrors()`, dan `ctx.Location()` (redirect eksternal) | P0 |
| VIEW-08 | Penanganan error validasi dan *error bag* bernama untuk halaman dengan banyak form | P1 |
| VIEW-09 | Halaman error kustom (404, 403, 500) sebagai komponen Vue | P1 |
| VIEW-10 | Pemulihan posisi scroll dan preservasi state komponen opsional (`preserveScroll`, `preserveState`) | P1 |
| VIEW-11 | Server-Side Rendering | P2 |

**Paket frontend `@vuepher/vue3`:**

| ID | Kebutuhan | Prioritas |
| --- | --- | --- |
| FE-01 | `createVuepherApp({ resolve, setup })` untuk bootstrap dengan resolver halaman via `import.meta.glob` | P0 |
| FE-02 | `<VuepherLink href method data preserve-scroll>` mencegat klik dan melakukan visit SPA | P0 |
| FE-03 | `useVuepherForm(initial)` mengembalikan `data`, `errors`, `processing`, `progress`, `post/put/patch/delete`, `reset`, `clearErrors`, `transform` | P0 |
| FE-04 | `usePage()` untuk akses props bersama dan URL aktif | P0 |
| FE-05 | `router.visit()` programatik, termasuk `router.reload()` | P0 |
| FE-06 | Progress bar navigasi dan dukungan upload file dengan indikator progres | P1 |
| FE-07 | Helper `route()` bertipe, hasil generate dari named routes Go | P1 |

Contoh penggunaan form:

```vue
<script setup lang="ts">
import { useVuepherForm } from '@vuepher/vue3'
const form = useVuepherForm({ title: '', body: '' })
</script>

<template>
  <form @submit.prevent="form.post('/posts')">
    <input v-model="form.data.title" />
    <span v-if="form.errors.title">{{ form.errors.title }}</span>
    <button :disabled="form.processing">Simpan</button>
  </form>
</template>
```

### 6.4 Modul CLI & Generator

Binary CLI tersedia sebagai `vuepher` dengan alias `vp`.

| Perintah | Fungsi | Prioritas |
| --- | --- | --- |
| `vuepher new <project>` | Membuat proyek baru: struktur Go, `go.mod`, Vite + Vue 3 + TS, `.env`, `git init`, instal dependensi. Opsi interaktif: database, starter kit autentikasi | P0 |
| `vuepher dev` | Menjalankan Go server dengan hot-reload dan Vite HMR secara paralel dalam satu terminal dengan log berwarna berprefiks | P0 |
| `vuepher build` | Build Vite → embed aset → kompilasi Go → output satu binary (opsi `--os`, `--arch` untuk cross-compile) | P0 (Phase 3 untuk embed penuh) |
| `vuepher make:controller <Nama> [--resource]` | Generate controller (opsi resource berisi 7 aksi CRUD) | P0 |
| `vuepher make:model <Nama> [-m] [-c] [-a]` | Generate model; `-m` migrasi, `-c` controller, `-a` semuanya | P0 |
| `vuepher make:middleware <Nama>` | Generate middleware | P0 |
| `vuepher make:view <Folder/File>` | Generate komponen `.vue` dengan blok `<script setup lang="ts">` yang sudah mengimpor tipe props dari hasil type-gen | P0 |
| `vuepher make:request`, `make:migration`, `make:seeder`, `make:factory`, `make:command` | Generator pendukung | P1 |
| `vuepher migrate[:rollback\|:fresh\|:status]`, `db:seed` | Manajemen database | P0 |
| `vuepher route:list` | Tabel seluruh route terdaftar | P1 |
| `vuepher types:generate` | Menjalankan type-gen manual (otomatis saat `dev`) | P1 |
| `vuepher stub:publish` | Menyalin stub ke `stubs/` proyek agar bisa dikustomisasi | P1 |
| `vuepher tinker` (REPL) | Eksplorasi interaktif model dan service | P2 |

Persyaratan generator:

- Stub berupa file `text/template` yang di-embed di binary CLI dan dapat di-override oleh `stubs/` di proyek.
- Generator tidak menimpa file yang sudah ada tanpa flag `--force`.
- Nama dinormalisasi otomatis (`BlogPost` → `blog_posts` untuk tabel, `PostController` untuk controller).
- `make:view` mendeteksi controller yang memanggil `ctx.View` dengan nama komponen yang sama dan menghubungkan tipe props secara otomatis bila tersedia.
- Pesan error CLI harus actionable (menjelaskan apa yang salah dan cara memperbaikinya).

### 6.5 Auto Type-Gen (Go struct → TypeScript)

| ID | Kebutuhan | Prioritas |
| --- | --- | --- |
| TG-01 | Membaca struct Go (via `go/ast` + `go/types`) dan menghasilkan `.d.ts` di `resources/js/types/generated/` | P1 |
| TG-02 | Pemetaan tipe: `string`→`string`, numerik→`number`, `bool`→`boolean`, `time.Time`→`string` (ISO 8601), `[]T`→`T[]`, `map[string]T`→`Record<string,T>`, pointer/`omitempty`→optional/nullable | P1 |
| TG-03 | Menghormati tag `json:"..."` untuk nama properti | P1 |
| TG-04 | Mendukung struct bersarang, embedded struct, generic sederhana, dan enum (const bertipe) menjadi union literal | P1 |
| TG-05 | Props bertipe per halaman: `vuepher.View[PostsIndexProps](ctx, "Posts/Index", props)` menghasilkan deklarasi `PageProps` untuk komponen tersebut | P1 |
| TG-06 | Mode watch: regenerasi inkremental saat file Go berubah selama `vuepher dev`, dengan latensi di bawah 1 detik | P1 |
| TG-07 | Tag opsional `ts:"..."` untuk override tipe (misalnya `ts:"type=Date"`) | P2 |
| TG-08 | Mode CI `types:generate --check` yang gagal bila file generated tidak sinkron | P2 |

## 7. Non-Functional Requirements

### 7.1 Performa

| Aspek | Target |
| --- | --- |
| Overhead framework per request (route + middleware default + render page object) | < 150 µs di atas router mentah (benchmark internal) |
| Throughput halaman JSON page object (hardware referensi 4 vCPU) | ≥ 30.000 req/detik tanpa DB |
| Waktu start aplikasi produksi | < 300 ms (tanpa migrasi) |
| Memori idle aplikasi contoh | < 40 MB RSS |
| Latensi hot-reload Go (simpan → server siap) | < 2 detik untuk proyek menengah |
| Latensi HMR Vue | Mengikuti performa Vite (< 200 ms) |
| Regenerasi type-gen inkremental | < 1 detik |

### 7.2 Ukuran Binary

- Binary produksi aplikasi starter (termasuk aset Vue ter-embed dan kompresi gzip/brotli pra-build): **< 25 MB**.
- Kontribusi framework terhadap ukuran binary (di luar aplikasi): **< 8 MB**.
- Opsi `--strip` dan `-ldflags="-s -w"` diaktifkan default pada `vuepher build`.

### 7.3 Kemudahan Setup & DX

- Prasyarat hanya Go ≥ 1.22 dan Node.js LTS (hanya untuk development dan build, tidak untuk runtime produksi).
- Instalasi CLI satu perintah: `go install github.com/vuepher/vuepher/cmd/vuepher@latest` serta skrip instalasi dan Homebrew.
- `vuepher doctor` memeriksa prasyarat (versi Go, Node, port, izin) dan memberi saran perbaikan.
- Dokumentasi berbahasa Inggris (utama) dengan terjemahan Bahasa Indonesia; setiap fitur memiliki contoh kode yang dapat dijalankan.
- Pesan error framework mencantumkan lokasi file dan saran perbaikan.

### 7.4 Keandalan, Keamanan, dan Kompatibilitas

- CSRF, XSS (escape default pada template root), dan session fixation ditangani secara default.
- Cookie session `HttpOnly`, `SameSite=Lax`, `Secure` di produksi.
- Dependensi dipindai (govulncheck, npm audit) di CI; kebijakan rilis keamanan terdokumentasi.
- Mengikuti Semantic Versioning; perubahan breaking hanya pada rilis mayor dengan panduan upgrade.
- Dukungan platform: Linux, macOS, Windows (WSL2 dan native) untuk CLI; target deployment amd64 dan arm64.
- Cakupan test unit framework core ≥ 80%; test integrasi end-to-end untuk View Bridge dengan browser headless.

## 8. Struktur Direktori Standar Proyek Vuepher

```
myapp/
├── app/
│   ├── controllers/        # Controller (PostController, dst.)
│   ├── models/             # Model + wrapper ORM
│   ├── middleware/         # Middleware kustom
│   ├── requests/           # Struct validasi request
│   ├── services/           # Logika bisnis (opsional)
│   └── providers/          # Registrasi service/container
├── bootstrap/
│   └── app.go              # Inisialisasi aplikasi
├── config/                 # app.go, database.go, session.go, vite.go
├── database/
│   ├── migrations/         # Migrasi berversi
│   ├── seeders/
│   └── factories/
├── routes/
│   └── web.go              # Definisi route
├── resources/
│   ├── views/
│   │   └── app.html        # Template root HTML
│   ├── js/
│   │   ├── app.ts          # Entry point (createVuepherApp)
│   │   ├── Pages/          # Komponen halaman (Posts/Index.vue)
│   │   ├── Components/     # Komponen UI reusable
│   │   ├── Layouts/        # Layout (AppLayout.vue)
│   │   └── types/
│   │       └── generated/  # .d.ts hasil type-gen (jangan edit manual)
│   └── css/
├── public/                 # Aset statis; output build Vite (public/build)
├── storage/                # Log, upload, cache (di-gitignore)
├── stubs/                  # Stub generator kustom (hasil stub:publish)
├── tests/
├── embed.go                # Direktif go:embed untuk public/build
├── main.go                 # Entry point aplikasi
├── vuepher.json            # Konfigurasi CLI (port dev, path, opsi build)
├── vite.config.ts
├── package.json
├── tsconfig.json
├── go.mod
└── .env
```

Konvensi: struktur mengikuti konvensi Go (package per direktori, tanpa siklus impor) sambil mempertahankan penamaan yang familier bagi developer Laravel.

## 9. Roadmap Pengembangan

### Phase 1: MVP (± 3–4 bulan)

**Tujuan:** membuktikan alur inti "controller Go → halaman Vue" dan DX generator.

- Core HTTP: router, grup, resource route, middleware pipeline, `Context`, validasi, session cookie, konfigurasi `.env`.
- View Bridge: `ctx.View`, template root, integrasi Vite dev/prod, shared props, redirect, error validasi.
- Paket `@vuepher/vue3` v0.x: `createVuepherApp`, `<VuepherLink>`, `useVuepherForm`, `usePage`, `router.visit`.
- CLI: `new`, `dev`, `make:controller`, `make:model`, `make:middleware`, `make:view`, `migrate`.
- Database: koneksi GORM (SQLite/PostgreSQL/MySQL), base model, migrasi dasar.
- Dokumentasi awal, contoh aplikasi blog, benchmark Chi vs Fiber untuk keputusan router final.

**Kriteria selesai:** demo CRUD end-to-end dibuat dalam < 15 menit dari nol; 20 early adopter berhasil menjalankan tutorial tanpa bantuan.

### Phase 2: Ecosystem & ORM (± 4–5 bulan)

**Tujuan:** membuat framework layak dipakai untuk aplikasi nyata.

- ORM wrapper lengkap: fluent query, relasi + eager loading, scope, accessor/mutator, paginator, soft delete, factory, seeder.
- Autentikasi: guard session, starter kit login/register/reset password (Vue), middleware `auth` dan `guest`.
- Session driver tambahan (database, Redis), CSRF lengkap, rate limiter, halaman error kustom.
- Fitur View Bridge lanjutan: partial reload, lazy props, versi aset + `409`, preserve scroll/state, upload file dengan progres.
- CLI tambahan: `route:list`, `make:request/migration/seeder/factory`, `stub:publish`, `doctor`.
- Named routes dan helper `route()` frontend.
- Situs dokumentasi, panduan migrasi "Dari Laravel ke Vuepher", kanal komunitas.
- Rilis **v0.9 (beta publik)**.

### Phase 3: Auto Type-Gen & Single Binary (± 3–4 bulan)

**Tujuan:** diferensiasi utama dan kesiapan v1.0.

- Auto Type-Gen penuh (TG-01 sampai TG-06) terintegrasi dengan `vuepher dev` dan `make:view`.
- `vuepher build` end-to-end: Vite build → `go:embed` → single binary, kompresi aset pra-build, cross-compile, serve aset ter-embed dengan cache header dan ETag.
- Optimasi performa dan ukuran binary sesuai target NFR.
- Audit keamanan, stabilisasi API, panduan upgrade, dan jaminan kompatibilitas SemVer.
- Rilis **v1.0**.

### Pasca-v1.0 (Backlog)

SSR opsional, queue/job + scheduler, broadcasting/WebSocket, plugin sistem, adapter Fiber, `tinker` REPL, mode CI type-gen, starter kit tambahan (admin panel, SaaS).

## 10. Risiko Teknis & Mitigasi

| # | Risiko | Dampak | Mitigasi |
| --- | --- | --- | --- |
| R1 | **Sinkronisasi proses Vite HMR dan Go server**: urutan start, race port, proses zombie saat Ctrl+C, restart Go yang memutus koneksi HMR | Tinggi: merusak DX inti | `vuepher dev` bertindak sebagai process supervisor (satu parent mengelola dua child). Health-check Vite sebelum Go melayani request. Port dibaca dari `vuepher.json` dengan fallback otomatis. Penanganan sinyal dan *process group kill* lintas OS. Hot-reload Go tidak menyentuh proses Vite, sehingga websocket HMR tetap hidup. Uji otomatis di Linux, macOS, Windows. |
| R2 | Latensi hot-reload Go lambat pada proyek besar | Sedang | Build inkremental dengan cache Go, debounce watcher, hanya rebuild package yang berubah, opsi mengecualikan direktori. |
| R3 | Kompleksitas dan keterbatasan type-gen (generic, interface, embedded, tipe kustom `MarshalJSON`) | Tinggi | Cakupan bertahap: dukung subset terdefinisi lebih dulu, dokumentasikan keterbatasan, sediakan tag override `ts:`, fallback `unknown` dengan peringatan jelas, test golden-file. |
| R4 | Protokol mirip Inertia menyimpang dari protokol resmi dan sulit dipelihara | Sedang | Spesifikasi protokol Vuepher ditulis eksplisit dan diversi. Test kontrak antara server Go dan paket frontend. Tidak mengklaim kompatibilitas penuh dengan adapter Inertia. |
| R5 | Wrapper ORM membocorkan abstraksi GORM atau membatasi fitur | Sedang | Wrapper tipis dan opt-in, selalu menyediakan akses `*gorm.DB`. Menghindari reimplementasi fitur GORM; fokus pada ergonomi. |
| R6 | Keterbatasan generics/refleksi Go membuat sintaks fluent (`Post::Where`) tidak seindah Eloquent | Sedang | Memakai generics Go 1.22+ (`Query[Post]()`) dan method chaining; menerima bahwa sintaks berbeda dari PHP namun konsisten dan type-safe. |
| R7 | Pilihan Chi vs Fiber salah dan sulit diganti | Sedang | Abstraksi `vuepher.Router`, benchmark di Phase 1, keputusan didokumentasikan sebagai ADR. |
| R8 | `go:embed` dan aset hasil build: path manifest salah, cache aset usang, ukuran binary membengkak | Sedang | Pipeline build deterministik, hashed filename dari Vite, ETag + immutable cache, kompresi pra-build, test integrasi yang menjalankan binary hasil build. |
| R9 | Keamanan: CSRF/session/XSS pada page object yang di-inline ke HTML | Tinggi | Serialisasi JSON aman (escape `<`, `>`, `&`, U+2028/2029) pada atribut `data-page`, CSRF default aktif, audit keamanan sebelum v1.0, kanal pelaporan kerentanan. |
| R10 | Adopsi rendah karena ekosistem kecil dan ketergantungan pada beberapa maintainer (bus factor) | Tinggi | Dokumentasi dan tutorial berkualitas, starter kit, kontribusi ramah pemula, tata kelola terbuka (CONTRIBUTING, roadmap publik), dan target merekrut minimal 3 maintainer inti. |
| R11 | Perbedaan lintas OS (path, sinyal proses, file watcher di Windows) | Sedang | Matriks CI lintas OS sejak Phase 1, abstraksi path/proses, dukungan WSL2 sebagai jalur resmi pada Windows. |
| R12 | Ketergantungan pada Node.js saat build membingungkan pengguna Go murni | Rendah | `vuepher doctor`, pesan error jelas, dokumentasi bahwa Node hanya dibutuhkan pada development/build; binary produksi tidak memerlukan Node. |

## 11. Pertanyaan Terbuka & Asumsi

1. **Router final:** Chi atau Fiber? (diputuskan setelah benchmark Phase 1)
2. **Sintaks ORM:** `Query[Post]()` generik atau pola statis berbasis method pada model? Perlu prototipe dan umpan balik developer Laravel.
3. **Migrasi:** berbasis file Go (type-safe) atau SQL mentah (lebih sederhana)? Rekomendasi awal: Go dengan dukungan SQL mentah.
4. **Hot-reload Go:** membangun watcher internal atau mengintegrasikan `air` sebagai dependensi?
5. **Kompatibilitas protokol:** apakah perlu kompatibel dengan klien Inertia resmi untuk memanfaatkan ekosistem yang ada?
6. **Tata kelola:** organisasi GitHub, model kontribusi, dan lisensi final (rencana: MIT).

**Asumsi:** developer target nyaman dengan Go dasar dan Vue 3 Composition API; aplikasi sasaran adalah monolit skala kecil sampai menengah; deployment utama berupa VPS atau container.
