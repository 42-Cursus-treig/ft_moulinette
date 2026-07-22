#include <unistd.h>
#include <stdlib.h>

void	ft_foreach(int *tab, int length, void(*f)(int));

static int	g_first = 1;

static void	putnbr(int n)
{
	char	buf[12];
	int		i;
	unsigned int	u;

	if (!g_first)
		write(1, " ", 1);
	g_first = 0;
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
	int	i;

	i = 0;
	while (i < argc - 1 && i < 64)
	{
		tab[i] = atoi(argv[i + 1]);
		i++;
	}
	ft_foreach(tab, i, &putnbr);
	return (0);
}
