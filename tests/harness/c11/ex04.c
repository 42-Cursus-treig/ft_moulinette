#include <unistd.h>
#include <stdlib.h>

int	ft_is_sort(int *tab, int length, int(*f)(int, int));

static int	cmp(int a, int b)
{
	return (a - b);
}

int	main(int argc, char **argv)
{
	int	tab[64];
	int	len;

	len = 0;
	while (len < argc - 1 && len < 64)
	{
		tab[len] = atoi(argv[len + 1]);
		len++;
	}
	if (ft_is_sort(tab, len, &cmp))
		write(1, "1", 1);
	else
		write(1, "0", 1);
	return (0);
}
