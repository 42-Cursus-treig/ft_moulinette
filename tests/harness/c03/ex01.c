#include <stdlib.h>
#include <unistd.h>

int	ft_strncmp(char *s1, char *s2, unsigned int n);

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
	unsigned int	n;

	if (argc < 4)
		return (1);
	n = (unsigned int)atoi(argv[3]);
	put_int(ft_strncmp(argv[1], argv[2], n));
	return (0);
}
