#include <stdio.h>
#include <unistd.h>
#include <stdlib.h>

int ft_ten_queens_puzzle(void);

int main(void)
{
    int p[2];
    
    // Création d'un pipe en mémoire au lieu d'un fichier sur le disque
    if (pipe(p) < 0)
        return (1);
        
    int stdout_save = dup(1);
    dup2(p[1], 1);
    
    // Exécution de l'exercice (724 lignes de 11 octets = 7964 octets, 
    // ce qui rentre largement dans le buffer natif du pipe)
    int ret = ft_ten_queens_puzzle();
    
    // Restauration de la sortie standard
    dup2(stdout_save, 1);
    close(stdout_save);
    
    // Fermeture indispensable de l'extrémité d'écriture pour que le read s'arrête
    close(p[1]); 
    
    char buf[8192] = {0};
    int n = 0;
    int r;
    
    // Lecture de tout le contenu généré
    while ((r = read(p[0], buf + n, sizeof(buf) - n - 1)) > 0)
        n += r;
        
    close(p[0]);

    // Affichage strict de la première et dernière ligne
    if (n >= 22) {
        write(1, buf, 11);
        write(1, buf + n - 11, 11);
    }
    
    // Affichage de la valeur de retour (724)
    printf("%d\n", ret);
    
    return (0);
}
