// relsign: alat rilis untuk pembaruan otomatis (hanya dipakai pemilik aplikasi, tidak ikut ke aplikasi).
//
//	relsign keygen <berkas-kunci-privat>            buat pasangan kunci; cetak kunci PUBLIK (base64)
//	relsign pubkey <berkas-kunci-privat>            cetak kunci publiknya
//	relsign sign   <berkas-kunci-privat> <tag> <zip>  tulis <zip>.sig
//	relsign verify <tag> <zip>                       cek <zip>.sig terhadap kunci publik yang TERTANAM di aplikasi
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"milesshoot/internal/update"
)

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "relsign: "+format+"\n", a...)
	os.Exit(1)
}

func loadKey(path string) ed25519.PrivateKey {
	b, err := os.ReadFile(path)
	if err != nil {
		die("kunci privat tidak terbaca: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		die("berkas kunci privat rusak")
	}
	return ed25519.PrivateKey(raw)
}

func main() {
	if len(os.Args) < 3 {
		die("pemakaian: keygen|pubkey|sign|verify — lihat komentar di cmd/relsign/main.go")
	}
	switch os.Args[1] {
	case "keygen":
		path := os.Args[2]
		if _, err := os.Stat(path); err == nil {
			die("%s sudah ada — tidak ditimpa (kunci lama bisa hilang selamanya)", path)
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			die("%v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			die("%v", err)
		}
		if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(priv)+"\n"), 0o600); err != nil {
			die("%v", err)
		}
		fmt.Println(base64.StdEncoding.EncodeToString(pub))
	case "pubkey":
		fmt.Println(base64.StdEncoding.EncodeToString(loadKey(os.Args[2]).Public().(ed25519.PublicKey)))
	case "sign":
		if len(os.Args) != 5 {
			die("pemakaian: sign <kunci> <tag> <zip>")
		}
		key, tag, zipPath := loadKey(os.Args[2]), os.Args[3], os.Args[4]
		f, err := os.Open(zipPath)
		if err != nil {
			die("%v", err)
		}
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			die("%v", err)
		}
		f.Close()
		sig := ed25519.Sign(key, update.SignedMessage(tag, filepath.Base(zipPath), hex.EncodeToString(h.Sum(nil))))
		if err := os.WriteFile(zipPath+".sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
			die("%v", err)
		}
		fmt.Println(zipPath + ".sig")
	case "verify":
		if len(os.Args) != 4 {
			die("pemakaian: verify <tag> <zip>")
		}
		tag, zipPath := os.Args[2], os.Args[3]
		pub, err := base64.StdEncoding.DecodeString(update.PublicKeyB64)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			die("kunci publik tertanam tidak valid")
		}
		sigB64, err := os.ReadFile(zipPath + ".sig")
		if err != nil {
			die("%v", err)
		}
		sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
		if err != nil {
			die("berkas .sig rusak")
		}
		f, err := os.Open(zipPath)
		if err != nil {
			die("%v", err)
		}
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			die("%v", err)
		}
		f.Close()
		if !ed25519.Verify(ed25519.PublicKey(pub), update.SignedMessage(tag, filepath.Base(zipPath), hex.EncodeToString(h.Sum(nil))), sig) {
			die("TANDA TANGAN TIDAK COCOK dengan kunci publik yang tertanam di aplikasi")
		}
		fmt.Println("tanda tangan cocok")
	default:
		die("perintah tidak dikenal: %s", os.Args[1])
	}
}
