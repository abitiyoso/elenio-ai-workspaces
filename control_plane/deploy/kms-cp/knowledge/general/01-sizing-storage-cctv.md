# Panduan Sizing Storage dan Server Rekaman CCTV

## Sumber dan cakupan halaman

Referensi umum untuk tim solusi saat menjawab KAK/RFP sistem CCTV atau Video Management System (VMS): cara menghitung kapasitas storage rekaman, bandwidth, dan kebutuhan server. Disusun 2026-10-31 dari praktik umum industri video surveillance. Angka bitrate di halaman ini adalah perkiraan awal (rule of thumb), bukan spesifikasi vendor; validasi selalu dengan kalkulator storage vendor VMS/kamera dan, bila ada, pengukuran bitrate kamera existing di lapangan.

Status: referensi umum (indikatif), belum menjadi standar resmi.

## Parameter yang harus diketahui sebelum menghitung

Kapasitas rekaman ditentukan oleh: jumlah kamera; resolusi; frame rate (fps); codec kompresi (H.264, H.265); kompleksitas adegan (area ramai, malam hari dengan noise, gerakan banyak menaikkan bitrate); mode rekam (kontinu 24 jam, berbasis gerak/motion, terjadwal); jam rekam per hari; lama retensi (hari); stream yang direkam (main stream atau juga sub stream); overhead RAID dan file system; cadangan pertumbuhan kamera.

Istilah "HD" di dokumen tender sering ambigu: bisa berarti 720p (1280×720) atau Full HD 1080p (1920×1080). Perbedaannya mengubah kebutuhan storage sekitar dua kali lipat, sehingga perlu dikonfirmasi saat klarifikasi (aanwijzing) atau dicantumkan sebagai asumsi tertulis.

## Rumus dasar kapasitas storage

Kapasitas per kamera per hari (GB) = bitrate (Mbps) × 10,8. Asal angka: 1 Mbps = 0,125 MB per detik × 86.400 detik = 10.800 MB ≈ 10,8 GB per hari untuk rekaman 24 jam.

Total kapasitas (TB, desimal) = bitrate (Mbps) × 10,8 × hari retensi × jumlah kamera ÷ 1.000, dikalikan faktor jam rekam bila tidak 24 jam dan faktor aktivitas bila rekam berbasis gerak.

Kapasitas di atas adalah kapasitas bersih (usable) yang dibutuhkan. Kapasitas disk mentah harus lebih besar karena overhead RAID, hot spare, dan cadangan ruang kosong (disarankan 10–20% agar file system dan proses penulisan tidak penuh).

## Bitrate tipikal per kamera (perkiraan awal)

Perkiraan untuk H.264, adegan kompleksitas sedang, rekam kontinu. Nilai aktual bisa lebih rendah atau jauh lebih tinggi tergantung kamera, pengaturan kualitas, dan adegan.

| Resolusi | Frame rate | Bitrate H.264 (Mbps) | Catatan |
|---|---|---|---|
| 720p (1 MP) | 12–16 fps | 1,0–1,5 | Sering disebut "HD" di dokumen lama |
| 1080p (2 MP) | 12–16 fps | 2,0–3,0 | Nilai tengah yang umum dipakai untuk perhitungan awal |
| 1080p (2 MP) | 25–30 fps | 4,0–6,0 | |
| 4 MP | 15 fps | 4,0–6,0 | |
| 4K (8 MP) | 15 fps | 8,0–12,0 | |

H.265 umumnya menghemat sekitar 30–50% bitrate dibanding H.264 pada kualitas yang setara, tetapi hanya bila kamera, VMS, dan client mendukungnya. Fitur "smart codec" vendor bisa menurunkan bitrate lebih jauh pada adegan statis.

## Tabel kapasitas per 100 kamera, retensi 30 hari, rekam 24 jam

| Bitrate per kamera | Per kamera per hari | Per kamera 30 hari | 100 kamera 30 hari |
|---|---|---|---|
| 1,0 Mbps | 10,8 GB | 324 GB | 32,4 TB |
| 1,5 Mbps | 16,2 GB | 486 GB | 48,6 TB |
| 2,0 Mbps | 21,6 GB | 648 GB | 64,8 TB |
| 2,5 Mbps | 27,0 GB | 810 GB | 81,0 TB |
| 4,0 Mbps | 43,2 GB | 1.296 GB | 129,6 TB |
| 6,0 Mbps | 64,8 GB | 1.944 GB | 194,4 TB |

Untuk jumlah kamera lain, kalikan secara proporsional (misalnya 300 kamera = 3 × nilai 100 kamera).

## Contoh perhitungan (ilustrasi umum)

Contoh: 300 kamera 1080p, 16 fps, H.264, diasumsikan 2,5 Mbps per kamera, rekam kontinu 24 jam, retensi 30 hari.

- Kapasitas bersih = 2,5 × 10,8 × 30 × 300 ÷ 1.000 = 243 TB.
- Tambah cadangan ruang kosong 15%: sekitar 280 TB usable.
- Dengan RAID 6 per grup 12 disk, kapasitas usable per grup = (12 − 2) × kapasitas disk. Dengan disk 16 TB, satu grup ≈ 160 TB usable, sehingga dibutuhkan 2 grup (24 disk) ditambah hot spare.
- Bila "HD" ternyata 720p pada 1,2 Mbps, kebutuhan bersih turun menjadi sekitar 117 TB.

Perhitungan ini menunjukkan mengapa asumsi resolusi dan bitrate harus ditulis eksplisit di proposal.

## Bandwidth dan throughput recording server

Bandwidth tulis total = jumlah bitrate semua kamera yang direkam. Contoh 300 kamera × 2,5 Mbps = 750 Mbps tulis kontinu, belum termasuk baca untuk playback, export, dan live view yang diteruskan server. Praktik umum: antarmuka jaringan 10 GbE untuk recording server dengan ratusan kamera, pemisahan jaringan kamera dan jaringan client/operator (VLAN atau NIC terpisah), dan perhitungan beban per recording server sesuai batas yang dipublikasikan vendor VMS (batas biasanya dinyatakan dalam jumlah kamera dan Mbps per server).

## Server manajemen dan recording server

Management server menyimpan konfigurasi, database pengguna, event, dan lisensi; kebutuhan storage-nya kecil tetapi harus andal (umumnya SSD dalam RAID 1, RAM 32–64 GB untuk ratusan kamera, sesuai rekomendasi vendor). Recording server menulis video ke storage lokal (DAS), NAS, atau SAN. Untuk ketersediaan tinggi gunakan failover recording server (N+1), RAID 6 atau RAID 10, catu daya ganda (dual PSU), dan UPS.

Disk untuk rekaman sebaiknya kelas surveillance atau enterprise yang dirancang untuk penulisan 24/7 (perhatikan workload rating TB per tahun dan garansi). Untuk disk berkapasitas besar, RAID 6 lebih disarankan daripada RAID 5 karena waktu rebuild yang panjang meningkatkan risiko kegagalan disk kedua.

## Pemutaran tanpa lag

Kebutuhan "tidak ada lag saat pemutaran rekaman" bergantung pada: throughput baca storage saat sedang menulis, beban CPU/GPU decoding di client, bandwidth jaringan client, dan apakah client memakai sub stream untuk tampilan multi-layar. Uji playback beberapa kamera bersamaan saat beban rekam penuh sebaiknya menjadi bagian dari testing & commissioning.

## Pertanyaan klarifikasi yang umum

- Apa arti "HD" (720p atau 1080p) dan berapa bitrate/pengaturan kualitas kamera existing?
- Apakah retensi berlaku untuk semua kamera, kontinu 24 jam, atau berbasis gerak?
- Codec apa yang didukung kamera existing (H.264 saja atau juga H.265)?
- Apakah dibutuhkan failover recording server atau cukup RAID?
- Berapa ruang rak, daya, dan port jaringan yang tersedia di ruang server existing?
- Berapa target pertumbuhan jumlah kamera dalam 3–5 tahun?

## Gap yang perlu divalidasi

Bitrate aktual setiap kamera existing harus diukur di lapangan atau dibaca dari konfigurasi VMS lama; angka di halaman ini hanya untuk estimasi awal. Batas kamera dan Mbps per recording server berbeda per vendor dan harus diambil dari dokumentasi vendor yang ditawarkan.
