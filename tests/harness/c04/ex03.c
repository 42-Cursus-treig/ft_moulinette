#include <unistd.h>

int	ft_atoi(char *str);

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
	if (argc < 2)
		return (1);
	put_int(ft_atoi(argv[1]));
	return (0);
}
