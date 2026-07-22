#include <unistd.h>

void	ft_sort_string_tab(char **tab);

static void	putstr_nl(char *s)
{
	int	i;

	i = 0;
	while (s[i])
		i++;
	write(1, s, i);
	write(1, "\n", 1);
}

int	main(int argc, char **argv)
{
	int	i;

	(void)argc;
	ft_sort_string_tab(argv + 1);
	i = 1;
	while (argv[i])
	{
		putstr_nl(argv[i]);
		i++;
	}
	return (0);
}
