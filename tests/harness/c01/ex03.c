#include <stdlib.h>
#include <unistd.h>

void	ft_div_mod(int a, int b, int *div, int *mod);

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
	int	div;
	int	mod;

	if (argc < 3)
		return (1);
	a = atoi(argv[1]);
	b = atoi(argv[2]);
	div = 0;
	mod = 0;
	ft_div_mod(a, b, &div, &mod);
	put_int(div);
	write(1, " ", 1);
	put_int(mod);
	return (0);
}
