<?php
$targetDir = "/tmp/uploads/";

$ftp_server = "ftp-server"; // nom du service docker-compose
$ftp_user   = "ftpuser";
$ftp_pass   = "motdepasse123";

$conn = ftp_connect($ftp_server);
if (!$conn) {
    die("Connexion FTP impossible.");
}
ftp_login($conn, $ftp_user, $ftp_pass);
ftp_pasv($conn, true); // mode passif recommandé entre conteneurs

foreach ($_FILES['fichiers']['tmp_name'] as $i => $tmpName) {
    $fileName = basename($_FILES['fichiers']['name'][$i]);
    $targetFile = $targetDir . $fileName;

    if (move_uploaded_file($tmpName, $targetFile)) {
        if (ftp_put($conn, "upload/" . $fileName, $targetFile, FTP_BINARY)) {
            echo "$fileName envoyé avec succès.<br>";
            unlink($targetFile); // nettoyage de l'espace temporaire
        } else {
            echo "Erreur d'envoi FTP pour $fileName.<br>";
        }
    }
}

ftp_close($conn);