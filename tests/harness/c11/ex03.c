#include <unistd.h>

int	ft_count_if(char **tab, int length, int(*f)(char*));

static int	non_vide(char *s)
{
	if (s && *s)
		return (1);
	return (0);
}

int	main(int argc, char **argv)
{
	int		n;
	char	buf[12];
	int		i;

	n = ft_count_if(argv + 1, argc - 1, &non_vide);
	i = 0;
	if (n == 0)
		buf[i++] = '0';
	while (n)
	{
		buf[i++] = '0' + (n % 10);
		n /= 10;
	}
	while (i--)
		write(1, &buf[i], 1);
	return (0);
}
