#include <stdio.h>
#include <stdlib.h>

int ft_is_prime(int nb);

int main(int argc, char **argv)
{
    if (argc == 2)
        printf("%d", ft_is_prime(atoi(argv[1])));
    return (0);
}
