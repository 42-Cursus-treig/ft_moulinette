#include <stdlib.h>
#include <unistd.h>

__attribute__((weak)) void	ft_putchar(char c)
{
	write(1, &c, 1);
}

void	ft_print_combn(int n);

int	main(int argc, char **argv)
{
	if (argc < 2)
		return (1);
	ft_print_combn(atoi(argv[1]));
	return (0);
}
