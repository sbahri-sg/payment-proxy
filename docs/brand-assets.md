# Brand assets dashboard

Dashboard menyimpan logo provider dan payment method secara lokal di `dashboard/public/brands`. Tidak ada hotlink saat halaman dibuka, sehingga tampilan tidak bergantung pada uptime situs pihak ketiga dan browser merchant tidak mengirim referer ke domain eksternal.

Logo hanya dipakai untuk identifikasi provider/payment channel pada control plane Emisell. Seluruh merek dagang tetap milik pemegang mereknya masing-masing. Jangan mengubah bentuk, warna, atau proporsi logo dan jangan memakai aset ini untuk menyiratkan endorsement/certification di luar status connector yang tercatat oleh Emisell.

## Sumber

Diambil dan diverifikasi pada 28 Agustus 2026:

| Kelompok | Sumber | Pemakaian |
|---|---|---|
| Xendit | [Official Xendit Logo Kit](https://www.xendit.co/en/company/asset-and-branding) | Monogram provider |
| Payment channels Xendit | [Official Xendit Partner Logo Kit](https://www.xendit.co/en/company/asset-and-branding) | QRIS, BCA, Mandiri, BNI, BRI, Permata, Visa, Mastercard, JCB, Amex, OVO, DANA, ShopeePay, LinkAja, Alfamart, Indomaret, Kredivo |
| Midtrans dan channel | [Midtrans official logo repository](https://github.com/veritrans/logo) | Midtrans, CIMB Niaga, Danamon, Maybank, ATM Bersama, GoPay, Akulaku |
| DOKU | [Official DOKU website](https://www.doku.com/en-us) | Provider DOKU dan DOKU payment methods |
| Duitku | [Official Duitku website](https://www.duitku.com/) | Provider Duitku |
| iPaymu | [Official iPaymu website](https://ipaymu.com/) | Provider iPaymu; logo dapat di-upload saat provider identity dibuat |
| AstraPay | [Official AstraPay website](https://www.astrapay.com/) | AstraPay |
| Jenius | [Official Jenius website](https://www.jenius.com/) | Jenius Pay |
| Indodana | [Official Indodana website](https://www.indodana.id/) | Indodana |
| Atome | [Official Atome website](https://www.atome.id/) | Atome |
| Bank Neo Commerce | [Official Bank Neo Commerce website](https://www.bankneocommerce.co.id/) | BNC Virtual Account |
| Bank Artha Graha | [Official Bank Artha Graha website](https://www.arthagraha.com/) | Artha Graha Virtual Account |
| Bank Sampoerna | [Official Bank Sampoerna website](https://www.banksampoerna.com/) | Sahabat Sampoerna Virtual Account |
| BSI, BTN, dan Bank Muamalat | [ZonaLogo BSI](https://zonalogo.com/id/logo-bank-bsi), [ZonaLogo BTN](https://zonalogo.com/id/logo-bank-btn), [ZonaLogo Bank Muamalat](https://zonalogo.com/id/logo-bank-muamalat) | Salinan logo terverifikasi terhadap identitas pada situs/laporan resmi; domain utama memblokir pengambilan aset otomatis |

Dokumentasi Midtrans menyatakan koleksi GitHub tersebut dapat dipakai di website merchant. Halaman brand Xendit juga menyediakan logo dan partner logo kit untuk tampilan integrasi/checkout.

## Aturan implementasi

- Seluruh aset SVG diperiksa agar tidak mengandung script, `foreignObject`, JavaScript URL, atau referensi gambar eksternal.
- Raster diperkecil maksimal 320 piksel agar katalog tidak mengunduh file multi-megabyte.
- Logo bawaan berada di mapping `dashboard/app/components/brand-logo.tsx`. Logo provider yang di-upload admin disimpan sebagai binary tervalidasi di database dan dipilih melalui canonical `code`, bukan path filesystem.
- Jika aset yang stabil belum tersedia, UI menampilkan fallback singkatan. Saat ini fallback dipakai untuk Pegadaian/Pos Indonesia dan Kartu Kredit Indonesia.
- Logo tidak menentukan dukungan teknis. Capability `DOCUMENTED` dan `CERTIFIED` dapat dipetakan; `DISABLED` tetap ditolak.

## Menambah atau memperbarui logo

Untuk provider baru, gunakan field `logo` saat **Add provider**. Dashboard menerima PNG/JPEG maksimum 512 KB dan 2048×2048 piksel; SVG upload ditolak untuk mencegah active content. Logo upload memiliki prioritas atas fallback/mapping bawaan.

Untuk aset bawaan yang dikelola bersama source code:

1. Ambil hanya dari brand owner atau koleksi yang secara eksplisit dirujuk oleh provider.
2. Simpan salinan lokal ke `dashboard/public/brands/providers` atau `dashboard/public/brands/payment-methods`.
3. Gunakan nama file lowercase tanpa spasi dan tambahkan mapping canonical code ke `brand-logo.tsx`.
4. Tambahkan sumber dan tanggal pengambilan ke dokumen ini.
5. Periksa SVG dan lakukan build dashboard sebelum deploy.
