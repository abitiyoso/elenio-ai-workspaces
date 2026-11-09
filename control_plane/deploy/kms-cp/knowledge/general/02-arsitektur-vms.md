# Arsitektur Referensi Video Management System (VMS) dan Migrasi Sistem CCTV

## Sumber dan cakupan halaman

Referensi umum, vendor-netral, untuk merancang atau mengevaluasi solusi Video Management System (VMS) pada pengadaan CCTV: komponen, integrasi, migrasi dari sistem lama, keamanan siber, pengujian, dan perangkat pendukung pemeliharaan. Disusun 2026-10-31 dari praktik umum industri. Fitur dan batas kapasitas produk tertentu harus diverifikasi pada datasheet vendor yang ditawarkan.

Status: referensi umum, belum menjadi standar resmi.

## Komponen utama arsitektur VMS

- Kamera IP: sumber video; sebaiknya mendukung ONVIF agar dapat dikelola VMS dari vendor berbeda.
- Jaringan: switch (sering PoE untuk kamera), VLAN terpisah untuk kamera, server, dan client; uplink serat optik untuk area luas.
- Management server: konfigurasi, pengguna dan hak akses, lisensi, event dan alarm, peta. Bisa dibuat redundan (failover/cluster).
- Recording server: menerima stream dan menulis rekaman ke storage; jumlahnya ditentukan beban kamera dan Mbps per server. Failover recording server (N+1) mengambil alih saat satu server gagal.
- Storage: DAS di recording server, NAS, atau SAN; dengan RAID dan hot spare. Lihat Panduan Sizing Storage dan Server Rekaman CCTV.
- Client: aplikasi desktop operator, web client, aplikasi mobile; workstation dengan GPU untuk banyak tampilan atau video wall.
- Video wall: tampilan bersama di ruang kontrol (command center, AOCC, ruang monitoring) dengan layout dan skema yang dapat dikonfigurasi.
- Layanan pendukung: sinkronisasi waktu (NTP) untuk semua kamera dan server, direktori pengguna (Active Directory/LDAP) bila tersedia, backup konfigurasi.

## Standar ONVIF yang relevan

ONVIF adalah standar antarmuka kamera IP dan VMS. Profil yang umum disebut di spesifikasi: Profile S (streaming video dasar dan PTZ), Profile T (streaming lanjutan termasuk H.265 dan pengaturan citra), Profile G (penyimpanan di edge/kartu SD dan pengambilan rekaman), Profile M (metadata dan analitik), serta Profile A dan C (kontrol akses) dan D (perangkat periferal kontrol akses). Klaim "ONVIF support" sebaiknya dirinci per profil dan dibuktikan dengan daftar kesesuaian (conformance) ONVIF.

## Fitur VMS yang sering diminta di KAK

Live view dan playback dalam satu client; manajemen dan inisialisasi perangkat; pencarian rekaman berdasarkan waktu atau event; export rekaman dengan watermark/tanda tangan digital; layout video wall; dewarping kamera fisheye; dukungan tampilan kamera 4K; manajemen level akses pengguna (role-based); integrasi kontrol akses (access control) dan video intercom; manajemen alarm dan peta; pemantauan jarak jauh; skalabilitas jumlah kamera dengan lisensi tambahan (upgradeable).

## Model lisensi VMS

Model umum: lisensi per kamera/channel (paling umum), lisensi server, lisensi client (sebagian produk menyediakan client tanpa batas), modul tambahan (video wall, analitik, integrasi), dan kontrak pemeliharaan perangkat lunak (Software Upgrade Plan/maintenance) untuk pembaruan versi. Saat KAK meminta "minimal N kamera, upgradeable" dan "unlimited client", pastikan penawaran menyebut jumlah lisensi kamera, edisi produk, batas maksimum edisi tersebut, dan masa berlaku maintenance.

## Migrasi dari sistem CCTV lama

Langkah umum migrasi ke VMS baru:

1. Inventaris kamera existing: lokasi, merek, model, firmware, resolusi, codec, stream, alamat IP, kredensial, status (berfungsi/rusak).
2. Uji kompatibilitas kamera dengan VMS baru (driver ONVIF atau driver native vendor) pada sampel tiap model.
3. Rencana lisensi: jumlah kamera existing ditambah ekspansi.
4. Rencana cut-over paralel: sistem lama tetap merekam sampai kamera dipindahkan dan rekaman di sistem baru terverifikasi, untuk menghindari jeda tanpa rekaman (blind spot).
5. Penanganan rekaman lama: ekspor rekaman yang masih wajib disimpan atau pertahankan server lama sampai masa retensinya habis.
6. Penggabungan beberapa sistem (misalnya CCTV dalam gedung dan CCTV kawasan) ke satu VMS: satukan skema penamaan kamera, grup, hak akses, dan peta.

Risiko umum: firmware kamera lama tidak kompatibel, kredensial kamera tidak diketahui, perangkat lama milik pihak ketiga yang harus dikembalikan, dan keterbatasan ruang rak, daya, atau port jaringan di ruang server existing.

## Keamanan siber CCTV/VMS

Praktik minimum: ganti password bawaan semua kamera dan server; perbarui firmware dan patch OS; matikan layanan yang tidak dipakai; segmentasi jaringan kamera dari jaringan kantor dan internet; akses berbasis peran (RBAC) dengan prinsip hak minimum; log audit akses dan export rekaman; sinkronisasi waktu untuk keabsahan bukti; enkripsi koneksi client–server bila didukung. Kebutuhan "remote monitoring via internet" sebaiknya diwujudkan melalui VPN atau gateway aman, bukan membuka port kamera atau server langsung ke internet. Organisasi pemilik biasanya memiliki kebijakan tata kelola TI sendiri yang perlu diikuti.

## Testing dan commissioning

Checklist umum: semua kamera terdaftar dan menampilkan video; rekaman berjalan untuk setiap kamera; verifikasi perhitungan retensi (kapasitas cukup untuk jumlah hari yang diminta pada bitrate aktual); uji playback beberapa kamera bersamaan saat beban rekam penuh; uji failover server dan rebuild RAID bila ada; uji export dan keaslian rekaman; uji level akses pengguna; sinkronisasi waktu; uji video wall dan integrasi (kontrol akses, intercom). Hasil dicatat dalam berita acara (BA) test & commissioning.

## Dokumen serah terima yang lazim

Softcopy installer server, recording, dan client; file atau sertifikat lisensi; backup konfigurasi; manual instalasi dan operasi; as-built (daftar perangkat, alamat IP, topologi); daftar akun dan level akses; laporan uji; materi pelatihan teknisi (instalasi, backup/restore, manajemen kamera, pengguna, rekaman) dan operator (pengoperasian client, playback, export).

## Perangkat pendukung pemeliharaan

KAK infrastruktur CCTV kadang meminta perangkat pendukung pemeliharaan:

- Laptop rugged/military grade: ketahanan biasanya dirujuk ke pengujian MIL-STD-810 (G atau H) dan rating IP terhadap debu/air; pastikan aplikasi client VMS terpasang dan diuji.
- OTDR (Optical Time Domain Reflectometer): alat uji serat optik untuk menemukan lokasi putus, sambungan, dan redaman. Parameter penting: panjang gelombang (1310/1550 nm untuk single-mode), jenis serat (single-mode ITU-T G.652), dynamic range, dead zone (event dan atenuasi), rentang jarak dan pulse width, format jejak Telcordia (.SOR), serta fitur power meter dan visual fault locator.

## Gap yang perlu divalidasi

Daftar kesesuaian kamera existing terhadap VMS yang ditawarkan, batas kapasitas per server, dan detail lisensi harus diambil dari vendor yang ditawarkan; halaman ini tidak memuat produk atau harga tertentu.
