#include <stdlib.h>
#include <unistd.h>

__attribute__((weak)) void	ft_putchar(char c)
{
	write(1, &c, 1);
}

void	ft_putnbr(int nb);

int	main(int argc, char **argv)
{
	if (argc < 2)
		return (1);
	ft_putnbr(atoi(argv[1]));
	return (0);
}
