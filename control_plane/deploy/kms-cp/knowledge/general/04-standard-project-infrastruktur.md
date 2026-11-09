# Standard Project — WBS Pengadaan dan Integrasi Infrastruktur

## Sumber dan cakupan halaman

Template Work Breakdown Structure (WBS) dan baseline mandays indikatif untuk proyek pengadaan dan integrasi infrastruktur TI berbasis hardware dan software (server, storage, VMS/CCTV, jaringan), sebagai pelengkap Standard Project untuk solusi digital. Disusun 2026-10-31 dari praktik umum proyek integrasi sistem. Angka mandays adalah contoh baseline indikatif, bukan standar yang sudah disahkan; validasi dengan Solution Architect dan Project Manager.

Status: contoh baseline indikatif.

## Peran

PM (Project Manager), SE (System/Solution Engineer), TEC (Teknisi), TR (Trainer, bisa dirangkap SE).

## Panduan kompleksitas

- Simple: satu lokasi, kurang dari 100 perangkat/kamera, tanpa migrasi dari sistem lama.
- Medium: satu lokasi, 100–300 perangkat/kamera, migrasi atau penggabungan sistem existing.
- Complex: lebih dari 300 perangkat/kamera, beberapa lokasi, failover/high availability, atau banyak integrasi (kontrol akses, intercom, analitik).

## Contoh WBS dan baseline mandays (MD)

| Aktivitas / deliverable | Peran | Simple | Medium | Complex |
|---|---|---|---|---|
| Kick-off, rencana kerja, jadwal, kurva S | PM | 1 | 2 | 3 |
| Survey lapangan dan pemeriksaan perangkat existing | SE, TEC | 2 | 4 | 8 |
| Desain solusi, sizing, dan Material Approval (MAP) | SE | 2 | 4 | 6 |
| Penerimaan material (MOS) dan staging/burn-in | TEC | 1 | 2 | 4 |
| Instalasi hardware di rak (server, storage, kabel) | TEC, SE | 2 | 4 | 8 |
| Instalasi OS, VMS/aplikasi, dan konfigurasi storage | SE | 2 | 4 | 6 |
| Integrasi/migrasi perangkat existing | SE, TEC | 2 | 6 | 12 |
| Konfigurasi pengguna, level akses, video wall | SE | 1 | 2 | 4 |
| Testing dan commissioning, berita acara | SE, TEC | 2 | 4 | 6 |
| Pelatihan teknisi dan operator | TR | 2 | 3 | 4 |
| Dokumentasi as-built dan serah terima | SE | 1 | 2 | 4 |

Total contoh baseline (tanpa PM berkelanjutan): Simple 18 MD, Medium 37 MD, Complex 65 MD. Tambahkan alokasi PM berkelanjutan sekitar 15–20% dari total untuk koordinasi, laporan progres, dan rapat dengan pengawas.

## Panduan integrasi perangkat existing

Untuk migrasi kamera atau perangkat ke sistem baru, gunakan acuan awal sekitar 1 MD per 25–50 perangkat yang kompatibel (pendaftaran, penamaan, pengaturan stream dan rekaman, verifikasi), dan lebih tinggi bila banyak perangkat butuh pembaruan firmware, kredensial tidak diketahui, atau perlu driver khusus.

## Masa pemeliharaan dan garansi

Masa pemeliharaan (misalnya 12 bulan) bukan mandays proyek tetapi biaya dukungan: kunjungan preventif berkala, penanganan gangguan on-site, dan penggantian suku cadang. Estimasi umum: kunjungan preventif per kuartal (1–2 MD per kunjungan) ditambah cadangan penanganan gangguan sesuai waktu respons yang dijanjikan.

## Jadwal dan lead time

Jadwal proyek harus memperhitungkan lead time pengadaan hardware (umumnya 2–4 minggu bila stok lokal, 4–12 minggu bila impor), waktu persetujuan MAP oleh pengawas, dan jendela kerja di lokasi operasional (kerja malam atau akhir pekan). Untuk kontrak berdurasi pendek, risiko lead time harus disebut di proposal.

## Gap yang perlu divalidasi

Rate per peran (talent rate card) dan standar mandays yang disahkan untuk proyek infrastruktur belum tersedia di Knowledge Base; angka di atas hanya baseline awal.
