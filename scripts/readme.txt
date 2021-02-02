# with go 1.15, we need to add buildmode to avoid C compiler error such as:
# ld.exe: error: export ordinal too large: 67011
# collect2.exe: error: ld returned 1 exit status
go build -buildmode=exe -o "C:\Users\cvila\AppData\Roaming\helm\plugins\helm-image\bin\helm-image.exe" main.go
