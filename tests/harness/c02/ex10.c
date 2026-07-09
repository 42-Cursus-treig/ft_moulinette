#include <stdlib.h>
#include <unistd.h>

unsigned int	ft_strlcpy(char *dest, char *src, unsigned int size);

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

static int	my_strlen(char *s)
{
	int	i;

	i = 0;
	while (s[i])
		i++;
	return (i);
}

int	main(int argc, char **argv)
{
	char			dest[256];
	unsigned int	size;
	unsigned int	ret;
	int				i;

	if (argc < 3)
		return (1);
	size = (unsigned int)atoi(argv[2]);
	i = 0;
	while (i < 256)
	{
		dest[i] = '\0';
		i++;
	}
	ret = ft_strlcpy(dest, argv[1], size);
	put_int((int)ret);
	write(1, " ", 1);
	write(1, dest, my_strlen(dest));
	return (0);
}
