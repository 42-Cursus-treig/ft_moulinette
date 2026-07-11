#include <stdlib.h>

void	ft_putnbr(int nb);

int	main(int argc, char **argv)
{
	if (argc < 2)
		return (1);
	ft_putnbr(atoi(argv[1]));
	return (0);
}
