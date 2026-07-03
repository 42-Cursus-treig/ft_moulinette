<?php
header('Content-Type: application/json');

$conn = ftp_connect("ftp-server");
ftp_login($conn, "ftpuser", "ft_moulinette4242");
ftp_pasv($conn, true);

$results = [];

foreach ($_FILES['fichiers']['tmp_name'] as $i => $tmpName) {
    $fileName = basename($_FILES['fichiers']['name'][$i]);
    $ok = ftp_put($conn, "upload/" . $fileName, $tmpName, FTP_BINARY);
    $results[] = ["name" => $fileName, "success" => $ok];
}

ftp_close($conn);
echo json_encode(["results" => $results]);