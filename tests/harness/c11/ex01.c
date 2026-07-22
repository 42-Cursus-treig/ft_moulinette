#include <unistd.h>
#include <stdlib.h>

int	*ft_map(int *tab, int length, int(*f)(int));

static int	twice(int n)
{
	return (n * 2);
}

static void	putnbr(int n, int first)
{
	char	buf[12];
	int		i;
	unsigned int	u;

	if (!first)
		write(1, " ", 1);
	if (n < 0)
	{
		write(1, "-", 1);
		u = -(unsigned int)n;
	}
	else
		u = n;
	i = 0;
	if (u == 0)
		buf[i++] = '0';
	while (u)
	{
		buf[i++] = '0' + (u % 10);
		u /= 10;
	}
	while (i--)
		write(1, &buf[i], 1);
}

int	main(int argc, char **argv)
{
	int	tab[64];
	int	*res;
	int	i;
	int	len;

	len = 0;
	while (len < argc - 1 && len < 64)
	{
		tab[len] = atoi(argv[len + 1]);
		len++;
	}
	res = ft_map(tab, len, &twice);
	if (!res)
		return (0);
	i = 0;
	while (i < len)
	{
		putnbr(res[i], i == 0);
		i++;
	}
	free(res);
	return (0);
}
