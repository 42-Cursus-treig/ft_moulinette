#include <stdlib.h>
#include <unistd.h>

void	ft_ultimate_div_mod(int *a, int *b);

static void	put_int(int n)
{
	char	c;

	if (n < 0)
	{
		c = '-';
		write(1, &c, 1);
		n = -n;
	}
	if (n >= 10)
		put_int(n / 10);
	c = '0' + (n % 10);
	write(1, &c, 1);
}

int	main(int argc, char **argv)
{
	int	a;
	int	b;

	if (argc < 3)
		return (1);
	a = atoi(argv[1]);
	b = atoi(argv[2]);
	ft_ultimate_div_mod(&a, &b);
	put_int(a);
	write(1, " ", 1);
	put_int(b);
	return (0);
}
